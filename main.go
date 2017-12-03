package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-kit/kit/endpoint"
	httptransport "github.com/go-kit/kit/transport/http"
)

// ProxyService provides control for HTTP-proxy.
type ProxyService interface {
	SetProperties(string) (bool, error)
	Start() int
	Stop() int
}

type proxyService struct{}

func (proxyService) SetProperties(s string) (bool, error) {
	if s == "" {
		return false, ErrEmpty
	}
	return true, nil
}

func (proxyService) Start() error {
	return nil
}

func (proxyService) Stop() error {
	return nil
}

func main() {
	svc := proxyService{}

	uppercaseHandler := httptransport.NewServer(
		makeSetPropertiesEndpoint(svc),
		decodeJsonStringRequest,
		okResponse,
	)

	startHandler := httptransport.NewServer(
		makeStartEndpoint(svc),
		decodeNoParamsRequest,
		okResponse,
	)

	stopHandler := httptransport.NewServer(
		makeStopEndpoint(svc),
		decodeNoParamsRequest,
		okResponse,
	)

	http.Handle("/set-properties", uppercaseHandler)
	http.Handle("/start", startHandler)
	http.Handle("/stop", stopHandler)
	go startProxy()
	log.Fatal(http.ListenAndServe(":8085", nil))
}

func makeSetPropertiesEndpoint(svc proxyService) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		req := request.(string)
		return svc.SetProperties(req)
	}
}

func makeStartEndpoint(svc proxyService) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		err := svc.Start()
		return nil, err
	}
}

func makeStopEndpoint(svc proxyService) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (interface{}, error) {
		err := svc.Stop()
		return nil, err
	}
}

func decodeJsonStringRequest(_ context.Context, r *http.Request) (interface{}, error) {
	return r.Body, nil
}

func decodeNoParamsRequest(_ context.Context, r *http.Request) (interface{}, error) {
	return nil, nil
}

func encodeResponse(_ context.Context, w http.ResponseWriter, response interface{}) error {
	return json.NewEncoder(w).Encode(response)
}

func okResponse(_ context.Context, w http.ResponseWriter, response interface{}) error {
	w.WriteHeader(http.StatusOK);
	return nil
}

type setPropertiesRequest struct {
	S string `json:"s"`
}

// ErrEmpty is returned when an input string is empty.
var ErrEmpty = errors.New("empty string")
