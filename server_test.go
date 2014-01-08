package dhcp4server

import (
	"encoding/json"
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
func ExampleServer() {
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

/*
 * Test Configuration Marshalling
 */
func TestConfigurationJSONMarshalling(test *testing.T) {
	var err error

	startConfiguration := Configuration{}
	startConfiguration.IP = net.IPv4(192, 168, 0, 1)
	startConfiguration.DefaultGateway = net.IPv4(192, 168, 0, 254)
	startConfiguration.DNSServers = []net.IP{net.IPv4(208, 67, 222, 222), net.IPv4(208, 67, 220, 220)}
	startConfiguration.SubnetMask = net.IPv4(255, 255, 255, 0)
	startConfiguration.LeaseDuration = 24 * time.Hour
	startConfiguration.IgnoreIPs = []net.IP{net.IPv4(192, 168, 0, 100)}

	startConfiguration.IgnoreHardwareAddress = make([]net.HardwareAddr, 0)
	exampleMac, err := net.ParseMAC("00:00:00:00:00:00")
	if err != nil {
		test.Error("Error Parsing Mac Address:" + err.Error())
	}
	startConfiguration.IgnoreHardwareAddress = append(startConfiguration.IgnoreHardwareAddress, exampleMac)

	test.Logf("Configuration Object:%v\n", startConfiguration)

	bytestartConfiguration, err := json.Marshal(startConfiguration)
	if err != nil {
		test.Error("Error Marshaling to JSON:" + err.Error())
	}

	test.Log("As JSON:" + string(bytestartConfiguration))

	endConfiguration := Configuration{}
	err = json.Unmarshal(bytestartConfiguration, &endConfiguration)
	if err != nil {
		test.Error("Error Unmarshaling to JSON:" + err.Error())
	}

	test.Logf("Configuration Object:%v\n", endConfiguration)
}
