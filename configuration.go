package dhcp4server

import (
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
