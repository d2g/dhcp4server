package dhcp4server

import (
	"encoding/json"
	"net"
	"time"
)

type Configuration struct {
	IP                    net.IP             //The IP Address We Tell Clients The Server Is On.
	DefaultGateway        net.IP             //The Default Gateway Address
	DNSServers            []net.IP           //DNS Servers
	SubnetMask            net.IP             //ie. 255.255.255.0
	LeaseDuration         time.Duration      //Number of Seconds
	IgnoreIPs             []net.IP           //Slice of IP's that should be ignored by the Server.
	IgnoreHardwareAddress []net.HardwareAddr //Slice of Hardware Addresses we should ignore.
}

func (t Configuration) MarshalJSON() ([]byte, error) {
	stringMarshal := struct {
		IP                    string
		DefaultGateway        string
		DNSServers            []string
		SubnetMask            string
		LeaseDuration         time.Duration
		IgnoreIPs             []string
		IgnoreHardwareAddress []string
	}{
		(t.IP.String()),
		(t.DefaultGateway.String()),
		make([]string, 0),
		(t.SubnetMask.String()),
		t.LeaseDuration,
		make([]string, 0),
		make([]string, 0),
	}

	for _, ip := range t.DNSServers {
		stringMarshal.DNSServers = append(stringMarshal.DNSServers, ip.String())
	}

	for _, ip := range t.IgnoreIPs {
		stringMarshal.IgnoreIPs = append(stringMarshal.IgnoreIPs, ip.String())
	}

	for _, hardwareAddress := range t.IgnoreHardwareAddress {
		stringMarshal.IgnoreHardwareAddress = append(stringMarshal.IgnoreHardwareAddress, hardwareAddress.String())
	}

	return json.Marshal(stringMarshal)
}

func (t *Configuration) UnmarshalJSON(data []byte) error {
	stringUnMarshal := struct {
		IP                    string
		DefaultGateway        string
		DNSServers            []string
		SubnetMask            string
		LeaseDuration         time.Duration
		IgnoreIPs             []string
		IgnoreHardwareAddress []string
	}{}

	err := json.Unmarshal(data, &stringUnMarshal)
	if err != nil {
		return err
	}

	t.IP = net.ParseIP(stringUnMarshal.IP)
	t.DefaultGateway = net.ParseIP(stringUnMarshal.DefaultGateway)
	t.SubnetMask = net.ParseIP(stringUnMarshal.SubnetMask)
	t.LeaseDuration = stringUnMarshal.LeaseDuration

	t.DNSServers = make([]net.IP, 0)
	for _, value := range stringUnMarshal.DNSServers {
		t.DNSServers = append(t.DNSServers, net.ParseIP(value))
	}

	t.IgnoreIPs = make([]net.IP, 0)
	for _, value := range stringUnMarshal.IgnoreIPs {
		t.IgnoreIPs = append(t.IgnoreIPs, net.ParseIP(value))
	}

	t.IgnoreHardwareAddress = make([]net.HardwareAddr, 0)
	for _, value := range stringUnMarshal.IgnoreHardwareAddress {
		tmpMACAddress, err := net.ParseMAC(value)
		if err != nil {
			return err
		}
		t.IgnoreHardwareAddress = append(t.IgnoreHardwareAddress, tmpMACAddress)
	}

	return nil
}
