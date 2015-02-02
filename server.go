package dhcp4server

import (
	"bytes"
	"errors"
	"log"
	"net"
	"time"

	"github.com/d2g/dhcp4"
	"github.com/d2g/dhcp4server/leasepool"
	"golang.org/x/net/ipv4"
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
	connection *ipv4.PacketConn
}

/*
 * Start The DHCP Server
 */
func (this *Server) ListenAndServe() error {
	var err error

	inboundAddress := net.UDPAddr{IP: net.IPv4(0, 0, 0, 0), Port: 67}

	connection, err := net.ListenPacket("udp4", inboundAddress.String())
	if err != nil {
		log.Printf("Debug: Error Returned From ListenPacket On \"%s\" Because of \"%s\"\n", inboundAddress.String(), err.Error())
		return err
	}
	this.connection = ipv4.NewPacketConn(connection)
	defer this.connection.Close()

	//We Currently Don't Use this Feature Which is the only bit that is Linux Only.
	//if err := this.connection.SetControlMessage(ipv4.FlagInterface, true); err != nil {
	//	return err
	//}

	//Make Our Buffer (Max Buffer is 574) "I believe this 576 size comes from RFC 791" - Random Mailing list quote of the day.
	buffer := make([]byte, 576)

	log.Println("Trace: DHCP Server Listening.")

	for {
	ListenForDHCPPackets:
		if this.shutdown {
			return nil
		}

		//Set Read Deadline
		this.connection.SetReadDeadline(time.Now().Add(time.Second))
		// Read Packet
		n, control_message, source, err := this.connection.ReadFrom(buffer)

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

			log.Println("Debug: Unexpect Error from Connection Read From:" + err.Error())
			return err
		}

		//We seem to have an issue with undersized packets?
		if n < 240 {
			log.Printf("Error: Invalid Packet Size \"%d\" Received:%v\n", n, buffer[:n])
			continue
		}

		//We should ignore some requests
		//It shouldn't be possible to ignore IP's because they shouldn't have them as we're the DHCP server.
		//However, they can have i.e. if you're the client & server :S.
		for _, ipToIgnore := range this.Configuration.IgnoreIPs {
			if ipToIgnore.Equal(source.(*net.UDPAddr).IP) {
				log.Println("Debug: Ignoring DHCP Request From IP:" + ipToIgnore.String())
				continue
			}
		}

		packet := dhcp4.Packet(buffer[:n])

		//We can ignore hardware addresses.
		//Usefull for ignoring a range of hardware addresses
		for _, hardwareAddressToIgnore := range this.Configuration.IgnoreHardwareAddress {
			if bytes.Equal(hardwareAddressToIgnore, packet.CHAddr()) {
				log.Println("Debug: Ignoring DHCP Request From Hardware Address:" + hardwareAddressToIgnore.String())
				continue
			}
		}

		log.Printf("Trace: Packet Received ID:%v\n", packet.XId())
		log.Printf("Trace: Packet Options:%v\n", packet.ParseOptions())
		log.Printf("Trace: Packet Client IP : %v\n", packet.CIAddr().String())
		log.Printf("Trace: Packet Your IP   : %v\n", packet.YIAddr().String())
		log.Printf("Trace: Packet Server IP : %v\n", packet.SIAddr().String())
		log.Printf("Trace: Packet Gateway IP: %v\n", packet.GIAddr().String())
		log.Printf("Trace: Packet Client Mac: %v\n", packet.CHAddr().String())

		//We need to stop butting in with other servers.
		if packet.SIAddr().Equal(net.IPv4(0, 0, 0, 0)) || packet.SIAddr().Equal(net.IP{}) || packet.SIAddr().Equal(this.Configuration.IP) {

			returnPacket, err := this.ServeDHCP(packet)
			if err != nil {
				log.Println("Debug: Error Serving DHCP:" + err.Error())
				return err
			}

			if len(returnPacket) > 0 {
				log.Printf("Trace: Packet Returned ID:%v\n", returnPacket.XId())
				log.Printf("Trace: Packet Options:%v\n", returnPacket.ParseOptions())
				log.Printf("Trace: Packet Client IP : %v\n", returnPacket.CIAddr().String())
				log.Printf("Trace: Packet Your IP   : %v\n", returnPacket.YIAddr().String())
				log.Printf("Trace: Packet Server IP : %v\n", returnPacket.SIAddr().String())
				log.Printf("Trace: Packet Gateway IP: %v\n", returnPacket.GIAddr().String())
				log.Printf("Trace: Packet Client Mac: %v\n", returnPacket.CHAddr().String())

				outboundAddress := net.UDPAddr{IP: net.IPv4bcast, Port: 68}
				_, err = this.connection.WriteTo(returnPacket, control_message, &outboundAddress)
				if err != nil {
					log.Println("Debug: Error Writing:" + err.Error())
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
			log.Println("Warning: It Looks Like Our Leases Are Depleted...")
			return dhcp4.Packet{}, nil
		}

		offerPacket := this.OfferPacket(packet)
		offerPacket.SetYIAddr(lease.IP)

		//Sort out the packet options
		offerPacket.PadToMinSize()

		lease.Status = leasepool.Reserved
		lease.MACAddress = packet.CHAddr()

		//If the lease expires within the next 5 Mins increase the lease expiary (Giving the Client 5 mins to complete)
		if lease.Expiry.Before(time.Now().Add(time.Minute * 5)) {
			lease.Expiry = time.Now().Add(time.Minute * 5)
		}

		if packetOptions[dhcp4.OptionHostName] != nil && string(packetOptions[dhcp4.OptionHostName]) != "" {
			lease.Hostname = string(packetOptions[dhcp4.OptionHostName])
		}

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
			log.Println("Warning: It Looks Like Our Leases Are Depleted...")
			return dhcp4.Packet{}, nil
		}

		//If the lease is not the one requested We should send a NAK..
		if len(packetOptions) > 0 && !net.IP(packetOptions[dhcp4.OptionRequestedIPAddress]).Equal(lease.IP) {
			//NAK
			declinePacket := this.DeclinePacket(packet)
			declinePacket.PadToMinSize()

			return declinePacket, nil
		} else {
			lease.Status = leasepool.Active
			lease.MACAddress = packet.CHAddr()

			lease.Expiry = time.Now().Add(this.Configuration.LeaseDuration)

			if packetOptions[dhcp4.OptionHostName] != nil && string(packetOptions[dhcp4.OptionHostName]) != "" {
				lease.Hostname = string(packetOptions[dhcp4.OptionHostName])
			}

			updated, err := this.LeasePool.UpdateLease(lease)
			if err != nil {
				return dhcp4.Packet{}, err
			}

			if updated {
				//ACK
				acknowledgementPacket := this.AcknowledgementPacket(packet)
				acknowledgementPacket.SetYIAddr(lease.IP)

				//Lease time.
				acknowledgementPacket.AddOption(dhcp4.OptionIPAddressLeaseTime, dhcp4.OptionsLeaseTime(lease.Expiry.Sub(time.Now())))
				acknowledgementPacket.PadToMinSize()

				return acknowledgementPacket, nil
			} else {
				//NAK
				declinePacket := this.DeclinePacket(packet)
				declinePacket.PadToMinSize()

				return declinePacket, nil
			}
		}
	case dhcp4.Decline:
		//Decline from the client:
		log.Printf("Debug: Decline Message:%v\n", packet)

	case dhcp4.Release:
		//Decline from the client:
		log.Printf("Debug: Release Message:%v\n", packet)

	default:
		log.Printf("Debug: Unexpected Packet Type:%v\n", dhcp4.MessageType(packetOptions[dhcp4.OptionDHCPMessageType][0]))
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
	offerPacket.SetGIAddr(discoverPacket.GIAddr())
	offerPacket.SetSecs(discoverPacket.Secs())

	//53
	offerPacket.AddOption(dhcp4.OptionDHCPMessageType, []byte{byte(dhcp4.Offer)})
	//54
	offerPacket.AddOption(dhcp4.OptionServerIdentifier, this.Configuration.IP.To4())
	//51
	offerPacket.AddOption(dhcp4.OptionIPAddressLeaseTime, dhcp4.OptionsLeaseTime(this.Configuration.LeaseDuration))

	//Other options go in requested order...
	discoverPacketOptions := discoverPacket.ParseOptions()

	ourOptions := make(dhcp4.Options)

	//1
	ourOptions[dhcp4.OptionSubnetMask] = this.Configuration.SubnetMask.To4()
	//3
	ourOptions[dhcp4.OptionRouter] = this.Configuration.DefaultGateway.To4()
	//6
	ourOptions[dhcp4.OptionDomainNameServer] = dhcp4.JoinIPs(this.Configuration.DNSServers)

	if discoverPacketOptions[dhcp4.OptionParameterRequestList] != nil {
		//Loop through the requested options and if we have them add them.
		for _, optionCode := range discoverPacketOptions[dhcp4.OptionParameterRequestList] {
			if !bytes.Equal(ourOptions[dhcp4.OptionCode(optionCode)], []byte{}) {
				offerPacket.AddOption(dhcp4.OptionCode(optionCode), ourOptions[dhcp4.OptionCode(optionCode)])
				delete(ourOptions, dhcp4.OptionCode(optionCode))
			}
		}
	}

	//Add all the options not requested.
	for optionCode, optionValue := range ourOptions {
		offerPacket.AddOption(optionCode, optionValue)
	}

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
	acknowledgementPacket.SetSecs(requestPacket.Secs())

	acknowledgementPacket.AddOption(dhcp4.OptionDHCPMessageType, []byte{byte(dhcp4.ACK)})
	acknowledgementPacket.AddOption(dhcp4.OptionSubnetMask, this.Configuration.SubnetMask.To4())
	acknowledgementPacket.AddOption(dhcp4.OptionRouter, this.Configuration.DefaultGateway.To4())
	acknowledgementPacket.AddOption(dhcp4.OptionDomainNameServer, dhcp4.JoinIPs(this.Configuration.DNSServers))
	acknowledgementPacket.AddOption(dhcp4.OptionServerIdentifier, this.Configuration.IP.To4())

	return acknowledgementPacket
}

/*
 * Create DHCP Decline
 */
func (this *Server) DeclinePacket(requestPacket dhcp4.Packet) dhcp4.Packet {

	declinePacket := dhcp4.NewPacket(dhcp4.BootReply)
	declinePacket.SetXId(requestPacket.XId())
	declinePacket.SetFlags(requestPacket.Flags())

	declinePacket.SetGIAddr(requestPacket.GIAddr())
	declinePacket.SetCHAddr(requestPacket.CHAddr())
	declinePacket.SetSecs(requestPacket.Secs())

	declinePacket.AddOption(dhcp4.OptionDHCPMessageType, []byte{byte(dhcp4.NAK)})
	declinePacket.AddOption(dhcp4.OptionSubnetMask, this.Configuration.SubnetMask.To4())
	declinePacket.AddOption(dhcp4.OptionRouter, this.Configuration.DefaultGateway.To4())
	declinePacket.AddOption(dhcp4.OptionDomainNameServer, dhcp4.JoinIPs(this.Configuration.DNSServers))
	declinePacket.AddOption(dhcp4.OptionServerIdentifier, this.Configuration.IP.To4())

	return declinePacket
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

/*
 * Garbage Collection
 * Run Garbage Collection On Your Leases To Free Expired Leases.
 */
func (t *Server) GC() error {
	leases, err := t.LeasePool.GetLeases()
	if err != nil {
		return err
	}

	for i := range leases {
		if leases[i].Status != leasepool.Free {
			//Lease Is Not Free

			if time.Now().After(leases[i].Expiry) {
				//Lease has expired.
				leases[i].Status = leasepool.Free
				updated, err := t.LeasePool.UpdateLease(leases[i])
				if err != nil {
					log.Printf("Warning: Error trying to Free Lease %s \"%v\"\n", leases[i].IP.To4().String(), err)
				}
				if !updated {
					log.Printf("Warning: Unable to Free Lease %s\n", leases[i].IP.To4().String())
				}
				continue
			}
		}
	}
	return nil
}
