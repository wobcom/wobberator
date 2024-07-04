package app

import (
	"context"
	"fmt"
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
