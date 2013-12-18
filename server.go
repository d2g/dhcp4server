package dhcp4server

import (
	"bytes"
	"errors"
	"github.com/d2g/dhcp4"
	"github.com/d2g/dhcp4server/leasepool"
	"log"
	"net"
	"sync"
	"time"
)

/*
 * The DHCP Server Structure
 */
type Server struct {
	Configuration *Configuration      //Server Configuration
	LeasePool     leasepool.LeasePool //Lease Pool Manager

	//Listeners & Response Connection.
	inbound *net.UDPConn

	//Outbound Connections are threaded.
	outbound      *net.UDPConn
	outboundMutex sync.Mutex
}

/*
 * Start The DHCP Server
 */
func (this *Server) ListenAndServe() error {
	var err error

	inboundAddress := net.UDPAddr{IP: net.IPv4(0, 0, 0, 0), Port: 67}
	this.inbound, err = net.ListenUDP("udp4", &inboundAddress)
	if err != nil {
		return err
	}
	defer this.inbound.Close()

	outboundAddress := net.UDPAddr{IP: net.IPv4bcast, Port: 68}
	this.outbound, err = net.DialUDP("udp4", nil, &outboundAddress)
	if err != nil {
		return err
	}
	defer this.outbound.Close()

	//Make Our Buffer (Max Buffer is 574) "I believe this 576 size comes from RFC 791" - Random Mailing list quote of the day.
	buffer := make([]byte, 576)
	for {
		//Listen For Packets
		n, source, err := this.inbound.ReadFrom(buffer)
		if err != nil {
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

		go func() {
			returnPacket, err := this.ServeDHCP(packet)
			if err != nil {
				log.Println("Error Serving DHCP:" + err.Error())
				return
			}

			if len(packet) > 0 {
				this.outboundMutex.Lock()
				this.outbound.Write(returnPacket)
				this.outboundMutex.Unlock()
			}
		}()
	}
}

func (this *Server) ServeDHCP(packet dhcp4.Packet) (dhcp4.Packet, error) {
	packetOptions := packet.ParseOptions()

	switch dhcp4.MessageType(packetOptions[dhcp4.OptionDHCPMessageType][0]) {
	case dhcp4.Discover:

		//Discover Received from client
		//Lets get the lease we're going to send them
		lease, err := this.LeasePool.GetLease(packet.CHAddr())
		if err != nil {
			//If we don't have a lease lets just be quiet about things.
			return dhcp4.Packet{}, err
		}

		offerPacket := this.OfferPacket(packet)

		offerPacket.SetYIAddr(lease.IP)
		offerPacket.PadToMinSize()

		lease.Status = leasepool.Reserved
		lease.MACAddress = packet.CHAddr()

		if packetOptions[dhcp4.OptionHostName] != nil && string(packetOptions[dhcp4.OptionHostName]) != "" {
			lease.Hostname = string(packetOptions[dhcp4.OptionHostName])
		}

		lease.Expiry = time.Now().Add(time.Duration(5) * time.Minute)

		reserved, err := this.LeasePool.ReserveLease(&lease)
		if err != nil {
			return dhcp4.Packet{}, err
		}

		if !reserved {
			//Unable to reserve lease (It's now active else where maybe?)
			return dhcp4.Packet{}, errors.New("Unable to Reserve Lease:" + lease.IP.String())
		}

		return offerPacket, nil
	case dhcp4.Request:
		//Request Received from client
		lease := leasepool.Lease{}

		lease.IP = net.IP(packetOptions[dhcp4.OptionRequestedIPAddress])
		lease.MACAddress = packet.CHAddr()
		lease.Status = leasepool.Active

		if packetOptions[dhcp4.OptionHostName] != nil && string(packetOptions[dhcp4.OptionHostName]) != "" {
			lease.Hostname = string(packetOptions[dhcp4.OptionHostName])
		}

		accepted, err := this.LeasePool.AcceptLease(&lease)
		if err != nil {
			return dhcp4.Packet{}, err
		}

		acknowledgementPacket := this.AcknowledgementPacket(packet)
		acknowledgementPacket.SetYIAddr(lease.IP)

		if accepted {
			//ACK
			acknowledgementPacket.AddOption(dhcp4.OptionDHCPMessageType, []byte{byte(dhcp4.ACK)})
			//Lease time.
			acknowledgementPacket.AddOption(dhcp4.OptionIPAddressLeaseTime, dhcp4.OptionsLeaseTime(lease.Expiry.Sub(time.Now())))
		} else {
			//NAK
			acknowledgementPacket.AddOption(dhcp4.OptionDHCPMessageType, []byte{byte(dhcp4.NAK)})
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

	offerPacket.SetGIAddr(discoverPacket.GIAddr())
	offerPacket.SetCHAddr(discoverPacket.CHAddr())
	offerPacket.SetSecs(discoverPacket.Secs())

	offerPacket.AddOption(dhcp4.OptionServerIdentifier, this.Configuration.IP.To4())
	offerPacket.AddOption(dhcp4.OptionSubnetMask, this.Configuration.SubnetMask.To4())
	offerPacket.AddOption(dhcp4.OptionRouter, this.Configuration.DefaultGateway.To4())
	offerPacket.AddOption(dhcp4.OptionDomainNameServer, dhcp4.JoinIPs(this.Configuration.DNSServers))
	offerPacket.AddOption(dhcp4.OptionDHCPMessageType, []byte{byte(dhcp4.Offer)})

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

	acknowledgementPacket.AddOption(dhcp4.OptionServerIdentifier, this.Configuration.IP.To4())
	acknowledgementPacket.AddOption(dhcp4.OptionSubnetMask, this.Configuration.SubnetMask.To4())
	acknowledgementPacket.AddOption(dhcp4.OptionRouter, this.Configuration.DefaultGateway.To4())
	acknowledgementPacket.AddOption(dhcp4.OptionDomainNameServer, dhcp4.JoinIPs(this.Configuration.DNSServers))

	return acknowledgementPacket
}
