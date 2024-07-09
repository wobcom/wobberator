package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/vishvananda/netlink"
	"github.com/wobcom/wobberator/internal/pkg/config"
	"k8s.io/client-go/kubernetes"
	"log/slog"
	"net/netip"
	"slices"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getAvailableAddreses(cidr string, alreadyUsedAddresses []netip.Addr) []netip.Addr {
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		panic(err)
	}
	p = p.Masked()
	addr := p.Addr()

	availableIPs := make([]netip.Addr, 0)

	for {
		if !p.Contains(addr) {
			break
		}

		if !slices.Contains(alreadyUsedAddresses, addr) {
			availableIPs = append(availableIPs, addr)
		}

		addr = addr.Next()
	}

	return availableIPs
}

func EnsureIPAddressesOnInterface(addresses []netip.Addr) error {

	link, err := netlink.LinkByName("antikubermatik")
	var linkNotFoundError netlink.LinkNotFoundError
	isLNF := errors.As(err, &linkNotFoundError)
	if isLNF {
		dummyAttrs := netlink.LinkAttrs{Name: "antikubermatik"}
		dummyInterface := netlink.Dummy{LinkAttrs: dummyAttrs}
		err := netlink.LinkAdd(&dummyInterface)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	existingAddresses, err := netlink.AddrList(link, netlink.FAMILY_V6)
	if err != nil {
		return err
	}

	for _, eAddress := range existingAddresses {
		netAddress := netip.MustParsePrefix(eAddress.String())
		if !slices.Contains(addresses, netAddress.Addr()) {
			slog.Info("Removing address from interface", "address", eAddress.String())
			err := netlink.AddrDel(link, &eAddress)
			if err != nil {
				return err
			}
		}
	}

	for _, ensureAddress := range addresses {
		netlinkAddress, err := netlink.ParseAddr(fmt.Sprintf("%v/128", ensureAddress.String()))
		if err != nil {
			return err
		}
		alreadyExists := false
		for _, eA := range existingAddresses {
			if netlinkAddress.IP.Equal(eA.IP) {
				alreadyExists = true
			}
		}

		if alreadyExists {
			continue
		}

		slog.Info("Adding address to interface", "address", netlinkAddress.String())
		err = netlink.AddrAdd(link, netlinkAddress)
		if err != nil {
			return err
		}
	}

	return nil
}

func RunHostRouteAssignment(ctx context.Context, clientset *kubernetes.Clientset, cfg *config.HostRouteAssignmentConfig) {
	for {

		allowedNets := make([]netip.Prefix, 0)
		for _, network := range cfg.ServiceNetworks {
			prefix := netip.MustParsePrefix(network)
			allowedNets = append(allowedNets, prefix)
		}

		services, err := clientset.CoreV1().Services("").List(ctx, metav1.ListOptions{})
		if err != nil {
			slog.Error("Unable to fetch services", "err", err)
		}

		activeServiceIPs := make([]netip.Addr, 0)

		slog.Info("Loaded services", "len", len(services.Items))

		for _, service := range services.Items {

			for _, ingress := range service.Status.LoadBalancer.Ingress {
				ip, err := netip.ParseAddr(ingress.IP)
				if err != nil {
					slog.Warn("ingress.IP was invalid", "err", err, "ip", ingress.IP)
				}

				isAllowed := false
				for _, net := range allowedNets {
					if net.Contains(ip) {
						isAllowed = true
					}
				}

				if !isAllowed {
					slog.Info("Skipping IP address", "ip", ip.String())
					continue
				}

				activeServiceIPs = append(activeServiceIPs, ip)

			}

		}

		err = EnsureIPAddressesOnInterface(activeServiceIPs)
		if err != nil {
			slog.Error("Error while ensuring ip addresses on interface", "err", err)
		}

		time.Sleep(10 * time.Second)
	}

}

func RunRouterIDAssignment(ctx context.Context, clientset *kubernetes.Clientset, cfg *config.RouterIDAssignmentConfig) {
	slog.Debug("Called RunRouterIDAssignment")

	for {
		slog.Debug("Fetching existing nodes...")
		nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			panic(err.Error())
		}

		for _, asnCfg := range cfg.ASNs {

			expectedVirtualRouterAnnotation := fmt.Sprintf("cilium.io/bgp-virtual-router.%s", asnCfg.ASN)

			annotationsPerHost := make(map[string]map[string]string)

			for _, node := range nodes.Items {

				annotationKeyValueMap := make(map[string]string)
				annotation, hasAnnotation := node.Annotations[expectedVirtualRouterAnnotation]

				if hasAnnotation {

					keyValuePairs := strings.Split(annotation, ",")
					for _, kvPair := range keyValuePairs {
						sKvPair := strings.Split(kvPair, "=")

						if len(sKvPair) != 2 {
							continue
						}

						key := strings.TrimSpace(sKvPair[0])
						value := strings.TrimSpace(sKvPair[1])
						annotationKeyValueMap[key] = value
					}
				}

				if annotationKeyValueMap != nil {
					slog.Debug("Fetched existing node annotations", "node", node.Name, "annotations", annotationKeyValueMap)
				}

				annotationsPerHost[node.Name] = annotationKeyValueMap
			}

			alreadyUsedRouterIDs := make([]netip.Addr, 0)

			for name, annotations := range annotationsPerHost {
				routerID, hasRouterId := annotations["router-id"]
				if hasRouterId {
					parsedIP, err := netip.ParseAddr(routerID)
					if err != nil {
						slog.Warn("router-id was invalid", "node", name, "router-id", routerID)
					} else {
						alreadyUsedRouterIDs = append(alreadyUsedRouterIDs, parsedIP)
					}
				}
			}

			availableRouterIds := getAvailableAddreses(asnCfg.Network, alreadyUsedRouterIDs)

			for _, n := range nodes.Items {
				node := n
				annotations := annotationsPerHost[node.Name]
				_, hasRouterId := annotations["router-id"]
				if !hasRouterId {
					var routerId netip.Addr
					routerId, availableRouterIds = availableRouterIds[0], availableRouterIds[1:]
					annotations["router-id"] = routerId.String()
				}

				virtualRouterAnnotationParts := make([]string, 0)

				for key, value := range annotationsPerHost[node.Name] {
					virtualRouterAnnotationParts = append(virtualRouterAnnotationParts, fmt.Sprintf("%s=%s", key, value))
				}
				virtualRouterAnnotation := strings.Join(virtualRouterAnnotationParts, ",")

				annotation, hasAnnotation := node.Annotations[expectedVirtualRouterAnnotation]

				if !hasAnnotation || annotation != virtualRouterAnnotation {
					slog.Info("Updating virtual router annotation", "node", node.Name, "annotation", virtualRouterAnnotation)
					node.Annotations[expectedVirtualRouterAnnotation] = virtualRouterAnnotation
					_, err = clientset.CoreV1().Nodes().Update(ctx, &node, metav1.UpdateOptions{})
					if err != nil {
						slog.Error("Nodes().Update() raised an error", "node", node.Name, "error", err)
					}
				}
			}
		}
		time.Sleep(10 * time.Second)
	}
}
