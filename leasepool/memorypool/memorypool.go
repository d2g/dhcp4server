package memorypool

import (
	"errors"
	"github.com/d2g/dhcp4server/leasepool"
	"net"
	"time"
)

type MemoryPool struct {
	Pool      []leasepool.Lease
	NextLease int
}

func (this *MemoryPool) AddToPool(ips []net.IP) error {
	if this.Pool == nil {
		this.Pool = make([]leasepool.Lease, 0)
	}

	for _, ip := range ips {

		existing := false
		for i, _ := range this.Pool {
			if this.Pool[i].IP.Equal(ip) {
				existing = true
				break
			}
		}

		if !existing {
			this.Pool = append(this.Pool, leasepool.Lease{IP: ip})
		}
	}

	return nil
}

func (this *MemoryPool) PurgePool() error {
	for i := (len(this.Pool) - 1); i <= 0; i-- {

		if i == 0 {
			this.Pool = []leasepool.Lease{}
		} else {
			this.Pool = this.Pool[0:i]
		}
	}

	return nil
}

func (this *MemoryPool) GetLease(hardwareAddress net.HardwareAddr) (leasepool.Lease, error) {
	nextDHCPLease := leasepool.Lease{}
	nextDHCPLease.MACAddress = hardwareAddress
	this.cleanupLeases(&nextDHCPLease)

	if len(this.Pool) == 0 {
		return nextDHCPLease, errors.New("Lease Pool Is Empty")
	}

	for i := 0; i < len(this.Pool); i++ {
		if this.Pool[this.NextLease+i].Status != leasepool.Active {
			//Lease is free lets use it.
			nextDHCPLease = this.Pool[this.NextLease+i]
			this.NextLease++

			if this.NextLease >= len(this.Pool) {
				this.NextLease = 0
			}
			return nextDHCPLease, nil
		}
	}

	return nextDHCPLease, errors.New("Lease Pool is Depleted")
}

func (this *MemoryPool) ReserveLease(reserveLease *leasepool.Lease) (bool, error) {
	this.cleanupLeases(reserveLease)

	//Reserve the Pool
	for i, _ := range this.Pool {
		if this.Pool[i].IP.Equal(reserveLease.IP) {
			if this.Pool[i].Status != leasepool.Active {
				//The Lease Is Not Active
				this.Pool[i].Status = leasepool.Reserved
				this.Pool[i].MACAddress = reserveLease.MACAddress

				return true, nil

			} else {
				//The Lease is Active
				if this.Pool[i].MACAddress.String() == reserveLease.MACAddress.String() {
					//It's ok it's My Lease
					return true, nil
				} else {
					//Ok it's not mine..
					return false, nil
				}
			}
		}
	}
	return false, errors.New("IP is Not In Pool")
}

func (this *MemoryPool) AcceptLease(acceptLease *leasepool.Lease) (bool, error) {
	this.cleanupLeases(acceptLease)

	for i, _ := range this.Pool {
		if this.Pool[i].IP.Equal(acceptLease.IP) {
			//This is the Lease We Requested.
			if this.Pool[i].MACAddress.String() == acceptLease.MACAddress.String() {
				//It's Our Lease We can Do What we want with it
				//Lets Renew it Quick.
				this.Pool[i].Hostname = acceptLease.Hostname
				this.Pool[i].Status = leasepool.Active
				this.Pool[i].Expiry = (time.Now()).Add(time.Hour * time.Duration(24))

				//We Need to let the client know we've updated the lease also.
				acceptLease.Expiry = this.Pool[i].Expiry

				return true, nil

			} else {
				//Hmm not our lease.
				if this.Pool[i].Status != leasepool.Active {
					//Nobody's Got it So You can have it quick.
					this.Pool[i].MACAddress = acceptLease.MACAddress
					this.Pool[i].Hostname = acceptLease.Hostname
					this.Pool[i].Status = leasepool.Active
					this.Pool[i].Expiry = (time.Now()).Add(time.Hour * time.Duration(24))

					//We Need to let the client know we've updated the lease also.
					acceptLease.Expiry = this.Pool[i].Expiry

					return true, nil

				} else {
					//It's not our lease and it's active elsewhere.
					//Then we can't use it.
					return false, nil
				}

			}

		}
	}
	return false, errors.New("IP is Not In Pool")
}

/*
 * If we are requesting a Lease but already have an Lease on a different IP then
 * we want to clear that old Lease.
 */
func (this *MemoryPool) cleanupLeases(requestedLease *leasepool.Lease) {

	for i, _ := range this.Pool {

		//Only Ever Bother to Clean Up Active or Reserved Leases
		if this.Pool[i].Status == leasepool.Active || this.Pool[i].Status == leasepool.Reserved {

			//Expired Leases
			if this.Pool[i].Expiry.Before(time.Now()) {
				this.Pool[i].Status = leasepool.Free

				//This Lease is now Free so move on.
				continue
			}

			//If the MAC is used by another Lease Release that Lease.
			if this.Pool[i].MACAddress.String() == requestedLease.MACAddress.String() && !this.Pool[i].IP.Equal(requestedLease.IP) {
				//The MAC is assigned to this IP but it's not requesting it.
				//So lets Clear it.
				this.Pool[i].Status = leasepool.Free
			}
		}
	}
}
