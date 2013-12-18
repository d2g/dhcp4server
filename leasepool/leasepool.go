package leasepool

import (
	"net"
)

/*
 * Notes: The LeasePool should be self managing.
 * When GetLease is Called if there are no Leases Available
 * it should free up it's own leases if possible.
 */
type LeasePool interface {
	//Add An Array of IP's to the Pool of Leases (This should add new but not replace existing leases).
	AddToPool([]net.IP) error
	//Remove All Leases from the Pool (Required for Persistant LeaseManagers)
	PurgePool() error
	//Get The Next Free lease or Get the lease already in use by that hardware address.
	GetLease(net.HardwareAddr) (Lease, error)
	//Reserve
	ReserveLease(*Lease) (bool, error)
	//Accept Lease
	AcceptLease(*Lease) (bool, error)
}
