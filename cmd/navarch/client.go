package main

import (
	"net/http"

	"github.com/NavarchProject/navarch/proto/protoconnect"
)

func newClient() protoconnect.ControlPlaneServiceClient {
	return protoconnect.NewControlPlaneServiceClient(
		http.DefaultClient,
		controlPlaneAddr,
	)
}

