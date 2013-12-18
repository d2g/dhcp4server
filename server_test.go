package dhcp4server

import (
	"github.com/d2g/dhcp4"
	"github.com/d2g/dhcp4server/leasepool/memorypool"
	"log"
	"net"
	"testing"
	"time"
)

/*
 * Example Server :D
 */
func Test_ExampleServer(test *testing.T) {
	//Configure the Server:
	myConfiguration := Configuration{}

	//You Should replace this with the IP of the Machine you are running the application on.
	myConfiguration.IP = net.IPv4(192, 168, 1, 201)

	//Replace this section with the subnet
	myConfiguration.SubnetMask = net.IPv4(255, 255, 255, 0)

	//Update these with your selected DNS Servers these belong to OpenDNS
	myConfiguration.DNSServers = []net.IP{net.IPv4(208, 67, 222, 222), net.IPv4(208, 67, 220, 220)}

	//Our Leases Are valid for 24 hours..
	myConfiguration.LeaseDuration = 24 * time.Hour

	//Create a Lease Pool We're going to use a memory pool
	//Remember the memory is cleared on restart so you will reissue the same IP Addresses.
	myMemoryLeasePool := memorypool.MemoryPool{}

	//Lets add a list of IPs to the pool these will be served to the clients so make sure they work for you.
	// So Create Array of IPs 192.168.1.1 to 192.168.1.30
	leaseIPs := make([]net.IP, 0)
	for i := 0; i < 30; i++ {
		leaseIPs = append(leaseIPs, dhcp4.IPAdd(net.IPv4(192, 168, 1, 1), i))
	}

	//Add those IPs to the lease pool
	err := myMemoryLeasePool.AddToPool(leaseIPs)
	if err != nil {
		log.Fatalln("Error Adding IPs to pool:" + err.Error())
	}

	//Create The Server
	myServer := Server{}
	myServer.Configuration = &myConfiguration
	myServer.LeasePool = &myMemoryLeasePool

	//Start the Server...
	err = myServer.ListenAndServe()
	if err != nil {
		log.Fatalln("Error Starting Server:" + err.Error())
	}
}
