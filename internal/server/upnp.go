package server

import (
	"context"
	"fmt"
	"time"

	"github.com/huin/goupnp/dcps/internetgateway2"
)

// UPnPMapping represents an active port mapping
type UPnPMapping struct {
	ExternalPort uint16
	InternalPort uint16
	ExternalIP   string
	client       interface{ DeletePortMapping(string, uint16, string) error }
}

// MapPort attempts to create a UPnP port mapping
func MapPort(internalPort uint16, description string) (*UPnPMapping, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try WANIPConnection2 first (newer)
	clients2, _, err := internetgateway2.NewWANIPConnection2ClientsCtx(ctx)
	if err == nil && len(clients2) > 0 {
		client := clients2[0]
		externalIP, err := client.GetExternalIPAddressCtx(ctx)
		if err == nil {
			// Try to add port mapping
			err = client.AddPortMappingCtx(
				ctx,
				"",              // NewRemoteHost (empty = any)
				internalPort,    // NewExternalPort
				"TCP",           // NewProtocol
				internalPort,    // NewInternalPort
				getLocalIPForUPnP(), // NewInternalClient
				true,            // NewEnabled
				description,     // NewPortMappingDescription
				0,               // NewLeaseDuration (0 = permanent)
			)
			if err == nil {
				return &UPnPMapping{
					ExternalPort: internalPort,
					InternalPort: internalPort,
					ExternalIP:   externalIP,
					client:       client,
				}, nil
			}
		}
	}

	// Fallback to WANIPConnection1
	clients1, _, err := internetgateway2.NewWANIPConnection1ClientsCtx(ctx)
	if err == nil && len(clients1) > 0 {
		client := clients1[0]
		externalIP, err := client.GetExternalIPAddressCtx(ctx)
		if err == nil {
			err = client.AddPortMappingCtx(
				ctx,
				"",
				internalPort,
				"TCP",
				internalPort,
				getLocalIPForUPnP(),
				true,
				description,
				0,
			)
			if err == nil {
				return &UPnPMapping{
					ExternalPort: internalPort,
					InternalPort: internalPort,
					ExternalIP:   externalIP,
					client:       client,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("no UPnP gateway found or port mapping failed")
}

// Close removes the UPnP port mapping
func (m *UPnPMapping) Close() error {
	if m.client == nil {
		return nil
	}
	return m.client.DeletePortMapping("", m.ExternalPort, "TCP")
}

// getLocalIPForUPnP gets the local IP for UPnP port mapping
func getLocalIPForUPnP() string {
	ip, err := GetLocalIP()
	if err != nil {
		return "0.0.0.0"
	}
	return ip
}

// TryMapPort attempts UPnP mapping and returns the result
// Returns (externalIP, mapped, error)
func TryMapPort(port uint16, description string) (string, bool, error) {
	mapping, err := MapPort(port, description)
	if err != nil {
		return "", false, err
	}
	return mapping.ExternalIP, true, nil
}
