package main

import (
	"crypto/tls"
	"net/http"

	"github.com/NavarchProject/navarch/proto/protoconnect"
)

func newClient() protoconnect.ControlPlaneServiceClient {
	httpClient := http.DefaultClient

	if insecure {
		httpClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		}
	}

	return protoconnect.NewControlPlaneServiceClient(
		httpClient,
		controlPlaneAddr,
	)
}