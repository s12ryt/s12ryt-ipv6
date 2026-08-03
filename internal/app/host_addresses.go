package app

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
)

func SystemHostAddresses() ([]netip.Addr, error) {
	return scanHostAddresses(systemInterfaceAddresses)
}

func scanHostAddresses(scan func() ([]net.Addr, error)) ([]netip.Addr, error) {
	if scan == nil {
		return nil, errors.New("interface address scanner is required")
	}
	addresses, err := scan()
	if err != nil {
		return nil, fmt.Errorf("list interface addresses: %w", err)
	}
	return collectHostAddresses(addresses)
}

func systemInterfaceAddresses() ([]net.Addr, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var result []net.Addr
	for _, networkInterface := range interfaces {
		addresses, err := networkInterface.Addrs()
		if err != nil {
			return nil, fmt.Errorf("list addresses for interface %q: %w", networkInterface.Name, err)
		}
		result = append(result, addresses...)
	}
	return result, nil
}

func collectHostAddresses(values []net.Addr) ([]netip.Addr, error) {
	unique := make(map[netip.Addr]struct{}, len(values))
	for _, value := range values {
		if value == nil {
			return nil, errors.New("interface address is nil")
		}
		address, err := parseNetAddress(value)
		if err != nil {
			return nil, err
		}
		unique[address.Unmap()] = struct{}{}
	}
	result := make([]netip.Addr, 0, len(unique))
	for address := range unique {
		result = append(result, address)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Compare(result[j]) < 0 })
	return result, nil
}

func parseNetAddress(value net.Addr) (netip.Addr, error) {
	var bytes []byte
	switch address := value.(type) {
	case *net.IPNet:
		bytes = address.IP
	case *net.IPAddr:
		if address.Zone != "" {
			return netip.Addr{}, errors.New("scoped interface address is not supported")
		}
		bytes = address.IP
	default:
		prefix, err := netip.ParsePrefix(value.String())
		if err != nil {
			address, addressErr := netip.ParseAddr(value.String())
			if addressErr != nil {
				return netip.Addr{}, fmt.Errorf("parse interface address: %w", err)
			}
			if address.Zone() != "" {
				return netip.Addr{}, errors.New("scoped interface address is not supported")
			}
			return address.Unmap(), nil
		}
		if prefix.Addr().Zone() != "" {
			return netip.Addr{}, errors.New("scoped interface address is not supported")
		}
		return prefix.Addr().Unmap(), nil
	}
	address, ok := netip.AddrFromSlice(bytes)
	if !ok {
		return netip.Addr{}, errors.New("interface address is invalid")
	}
	return address.Unmap(), nil
}
