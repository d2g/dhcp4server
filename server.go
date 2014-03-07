package dhcp4server

import (
	"bytes"
	"errors"
	"github.com/d2g/dhcp4"
	"github.com/d2g/dhcp4server/leasepool"
	"log"
	"net"
	"time"
)

/*
 * The DHCP Server Structure
 */
type Server struct {
	Configuration *Configuration      //Server Configuration
	LeasePool     leasepool.LeasePool //Lease Pool Manager

	//Used to Gracefully Close the Server
	shutdown bool

	//Listeners & Response Connection.
	connection *net.UDPConn
}

/*
 * Start The DHCP Server
 */
func (this *Server) ListenAndServe() error {
	var err error

	//this.shutdown = make(chan bool)
	//defer close(this.shutdown)

	inboundAddress := net.UDPAddr{IP: net.IPv4(0, 0, 0, 0), Port: 67}
	outboundAddress := net.UDPAddr{IP: net.IPv4bcast, Port: 68}
	this.connection, err = net.ListenUDP("udp4", &inboundAddress)
	if err != nil {
		return err
	}
	defer this.connection.Close()

	//Make Our Buffer (Max Buffer is 574) "I believe this 576 size comes from RFC 791" - Random Mailing list quote of the day.
	buffer := make([]byte, 576)

	for {
	ListenForDHCPPackets:
		if this.shutdown {
			return nil
		}

		//Set Read Deadline
		this.connection.SetReadDeadline(time.Now().Add(time.Second))
		// Read Packet
		n, source, err := this.connection.ReadFrom(buffer)
		if err != nil {

			switch v := err.(type) {
			case *net.OpError:
				if v.Timeout() {
					goto ListenForDHCPPackets
				}
			case *net.AddrError:
				if v.Timeout() {
					goto ListenForDHCPPackets
				}
			case *net.UnknownNetworkError:
				if v.Timeout() {
					goto ListenForDHCPPackets
				}
			}

			log.Println("Error:" + err.Error())
			return err
		}

		//We should ignore some requests
		//It shouldn't be possible to ignore IP's because they shouldn't have them as we're the DHCP server.
		//However, they can have i.e. if you're the client & server :S.
		for _, ipToIgnore := range this.Configuration.IgnoreIPs {
			if ipToIgnore.Equal(source.(*net.UDPAddr).IP) {
				log.Println("Ignoring DHCP Request From IP:" + ipToIgnore.String())
				continue
			}
		}

		packet := dhcp4.Packet(buffer[:n])

		//We can ignore hardware addresses.
		//Usefull for ignoring a range of hardware addresses
		for _, hardwareAddressToIgnore := range this.Configuration.IgnoreHardwareAddress {
			if bytes.Equal(hardwareAddressToIgnore, packet.CHAddr()) {
				log.Println("Ignoring DHCP Request From Hardware Address:" + hardwareAddressToIgnore.String())
				continue
			}
		}

		//We need to stop butting in with other servers.
		if packet.SIAddr().Equal(net.IPv4(0, 0, 0, 0)) || packet.SIAddr().Equal(net.IP{}) || packet.SIAddr().Equal(this.Configuration.IP) {

			returnPacket, err := this.ServeDHCP(packet)
			if err != nil {
				log.Println("Error Serving DHCP:" + err.Error())
				return err
			}

			if len(packet) > 0 {
				_, err := this.connection.WriteTo(returnPacket, &outboundAddress)
				if err != nil {
					log.Println("Error Writing:" + err.Error())
					return err
				}
			}
		}

	}
}

func (this *Server) ServeDHCP(packet dhcp4.Packet) (dhcp4.Packet, error) {
	packetOptions := packet.ParseOptions()

	switch dhcp4.MessageType(packetOptions[dhcp4.OptionDHCPMessageType][0]) {
	case dhcp4.Discover:

		//Discover Received from client
		//Lets get the lease we're going to send them
		found, lease, err := this.GetLease(packet)
		if err != nil {
			return dhcp4.Packet{}, err
		}

		if !found {
			log.Println("It Looks Like Our Leases Are Depleted...")
			return dhcp4.Packet{}, nil
		}

		offerPacket := this.OfferPacket(packet)
		offerPacket.SetYIAddr(lease.IP)
		offerPacket.PadToMinSize()

		lease.Reserve()
		lease.MACAddress = packet.CHAddr()

		if packetOptions[dhcp4.OptionHostName] != nil && string(packetOptions[dhcp4.OptionHostName]) != "" {
			lease.Hostname = string(packetOptions[dhcp4.OptionHostName])
		}

		//TODO: Fix
		lease.Expiry = time.Now().Add(time.Duration(5) * time.Minute)

		updated, err := this.LeasePool.UpdateLease(lease)
		if err != nil {
			return dhcp4.Packet{}, err
		}

		if !updated {
			//Unable to reserve lease (It's now active else where maybe?)
			return dhcp4.Packet{}, errors.New("Unable to Reserve Lease:" + lease.IP.String())
		}

		return offerPacket, nil
	case dhcp4.Request:
		//Request Received from client
		//Lets get the lease we're going to send them
		found, lease, err := this.GetLease(packet)
		if err != nil {
			return dhcp4.Packet{}, err
		}

		if !found {
			log.Println("It Looks Like Our Leases Are Depleted...")
			return dhcp4.Packet{}, nil
		}

		acknowledgementPacket := this.AcknowledgementPacket(packet)

		//If the lease is not the one requested We should send a NAK..
		if len(packetOptions) > 0 && !net.IP(packetOptions[dhcp4.OptionRequestedIPAddress]).Equal(lease.IP) {
			//NAK
			acknowledgementPacket.AddOption(dhcp4.OptionDHCPMessageType, []byte{byte(dhcp4.NAK)})
		} else {
			lease.Active()
			lease.MACAddress = packet.CHAddr()

			//TODO: Fix
			lease.Expiry = time.Now().Add(time.Duration(24) * time.Minute)

			if packetOptions[dhcp4.OptionHostName] != nil && string(packetOptions[dhcp4.OptionHostName]) != "" {
				lease.Hostname = string(packetOptions[dhcp4.OptionHostName])
			}

			updated, err := this.LeasePool.UpdateLease(lease)
			if err != nil {
				return dhcp4.Packet{}, err
			}

			if updated {
				//ACK
				acknowledgementPacket.SetYIAddr(lease.IP)
				acknowledgementPacket.AddOption(dhcp4.OptionDHCPMessageType, []byte{byte(dhcp4.ACK)})
				//Lease time.
				acknowledgementPacket.AddOption(dhcp4.OptionIPAddressLeaseTime, dhcp4.OptionsLeaseTime(lease.Expiry.Sub(time.Now())))
			} else {
				//NAK
				acknowledgementPacket.AddOption(dhcp4.OptionDHCPMessageType, []byte{byte(dhcp4.NAK)})
			}
		}

		acknowledgementPacket.PadToMinSize()

		return acknowledgementPacket, nil
	case dhcp4.Decline:
		//Decline from the client:
		log.Printf("Decline Message:%v\n", packet)

	case dhcp4.Release:
		//Decline from the client:
		log.Printf("Release Message:%v\n", packet)

	default:
		log.Printf("Unexpected Packet Type:%v\n", dhcp4.MessageType(packetOptions[dhcp4.OptionDHCPMessageType][0]))
	}

	return dhcp4.Packet{}, nil
}

/*
 * Create DHCP Offer Packet
 */
func (this *Server) OfferPacket(discoverPacket dhcp4.Packet) dhcp4.Packet {

	offerPacket := dhcp4.NewPacket(dhcp4.BootReply)
	offerPacket.SetXId(discoverPacket.XId())
	offerPacket.SetFlags(discoverPacket.Flags())

	offerPacket.SetCHAddr(discoverPacket.CHAddr())
	offerPacket.SetSIAddr(this.Configuration.IP)
	offerPacket.SetGIAddr(discoverPacket.GIAddr())
	offerPacket.SetSecs(discoverPacket.Secs())

	offerPacket.AddOption(dhcp4.OptionServerIdentifier, this.Configuration.IP.To4())
	offerPacket.AddOption(dhcp4.OptionSubnetMask, this.Configuration.SubnetMask.To4())
	offerPacket.AddOption(dhcp4.OptionRouter, this.Configuration.DefaultGateway.To4())
	offerPacket.AddOption(dhcp4.OptionDomainNameServer, dhcp4.JoinIPs(this.Configuration.DNSServers))
	offerPacket.AddOption(dhcp4.OptionDHCPMessageType, []byte{byte(dhcp4.Offer)})
	offerPacket.AddOption(dhcp4.OptionIPAddressLeaseTime, dhcp4.OptionsLeaseTime(this.Configuration.LeaseDuration*time.Second))

	return offerPacket

}

/*
 * Create DHCP Acknowledgement
 */
func (this *Server) AcknowledgementPacket(requestPacket dhcp4.Packet) dhcp4.Packet {

	acknowledgementPacket := dhcp4.NewPacket(dhcp4.BootReply)
	acknowledgementPacket.SetXId(requestPacket.XId())
	acknowledgementPacket.SetFlags(requestPacket.Flags())

	acknowledgementPacket.SetGIAddr(requestPacket.GIAddr())
	acknowledgementPacket.SetCHAddr(requestPacket.CHAddr())
	acknowledgementPacket.SetSIAddr(this.Configuration.IP)
	acknowledgementPacket.SetSecs(requestPacket.Secs())

	acknowledgementPacket.AddOption(dhcp4.OptionServerIdentifier, this.Configuration.IP.To4())
	acknowledgementPacket.AddOption(dhcp4.OptionSubnetMask, this.Configuration.SubnetMask.To4())
	acknowledgementPacket.AddOption(dhcp4.OptionRouter, this.Configuration.DefaultGateway.To4())
	acknowledgementPacket.AddOption(dhcp4.OptionDomainNameServer, dhcp4.JoinIPs(this.Configuration.DNSServers))
	acknowledgementPacket.AddOption(dhcp4.OptionIPAddressLeaseTime, dhcp4.OptionsLeaseTime(this.Configuration.LeaseDuration*time.Second))

	return acknowledgementPacket
}

/*
 * Get Lease tries to work out the best lease for the packet supplied.
 * Taking into account all Requested IP, Exisitng MACAddresses and Free leases.
 */
func (t *Server) GetLease(packet dhcp4.Packet) (found bool, lease leasepool.Lease, err error) {
	packetOptions := packet.ParseOptions()

	//Requested an IP
	if (len(packetOptions) > 0) &&
		packetOptions[dhcp4.OptionRequestedIPAddress] != nil &&
		!net.IP(packetOptions[dhcp4.OptionRequestedIPAddress]).Equal(net.IP{}) {
		//An IP Has Been Requested Let's Try and Get that One.

		found, lease, err = t.LeasePool.GetLease(net.IP(packetOptions[dhcp4.OptionRequestedIPAddress]))
		if err != nil {
			return
		}

		if found {
			if lease.Status == leasepool.Free {
				//Lease Is Free you Can Have it.
				return
			}
			if lease.Status != leasepool.Free && bytes.Equal(lease.MACAddress, packet.CHAddr()) {
				//Lease isn't free but it's yours
				return
			}
		}
	}

	//Ok Even if you requested an IP you can't have it.
	found, lease, err = t.LeasePool.GetLeaseForHardwareAddress(packet.CHAddr())
	if found || err != nil {
		return
	}

	//Just get the next free lease if you can.
	found, lease, err = t.LeasePool.GetNextFreeLease()
	return
}

/*
 * Shutdown The Server Gracefully
 */
func (t *Server) Shutdown() {
	t.shutdown = true
}
