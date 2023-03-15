package httpclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"bastionzero.com/bzerolib/tests"
	"github.com/cenkalti/backoff/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestHttpClient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "HttpClient Suite")
}

var _ = Describe("HttpClient", func() {
	var client *HttpClient
	var server *tests.MockServer

	ctx := context.Background()

	Context("Creation", func() {
		testUrl := "http://localhost"

		When("Creating with an endpoint", func() {
			var err error

			fakeEndpoint := "fake"

			BeforeEach(func() {
				client, err = New(testUrl, HTTPOptions{
					Endpoint: fakeEndpoint,
				})
			})

			It("can correctly build the full URL", func() {
				Expect(err).ToNot(HaveOccurred(), "Client failed to build correctly: %s", err)

				annotation := fmt.Sprintf("Client should have combined the testUrl with the provided endpoint but instead built: %s", client.targetUrl)
				Expect(client.targetUrl).To(Equal(fmt.Sprintf("%s/%s", testUrl, fakeEndpoint)), annotation)
			})
		})

		When("Creating with params", func() {
			var err error

			fakeParamKey := "fake"
			fakeParamValue := "fakeparam"

			fakeParams := url.Values{
				fakeParamKey: {fakeParamValue},
			}

			verifyParams := func(w http.ResponseWriter, r *http.Request) {
				p := r.URL.Query().Get(fakeParamKey)
				if p == fakeParamValue {
					w.WriteHeader(http.StatusOK)
				} else {
					w.WriteHeader(http.StatusBadRequest)
				}
			}

			BeforeEach(func() {
				server = tests.NewMockServer(tests.MockHandler{
					Endpoint:    "/",
					HandlerFunc: verifyParams,
				})

				client, _ = New(server.Url, HTTPOptions{
					Params: fakeParams,
				})
				_, err = client.Get(ctx)
			})

			It("includes those params in requests", func() {
				Expect(err).ToNot(HaveOccurred(), "Server did not see the param values we sent")
			})
		})

		When("Creating with headers", func() {
			var err error

			fakeHeaderKey := "fake"
			fakeHeaderValue := "fakeparam"

			fakeHeaders := http.Header{
				fakeHeaderKey: {fakeHeaderValue},
			}

			verifyHeaders := func(w http.ResponseWriter, r *http.Request) {
				h := r.Header.Get(fakeHeaderKey)
				if h == fakeHeaderValue {
					w.WriteHeader(http.StatusOK)
				} else {
					w.WriteHeader(http.StatusBadRequest)
				}
			}

			BeforeEach(func() {
				server = tests.NewMockServer(tests.MockHandler{
					Endpoint:    "/",
					HandlerFunc: verifyHeaders,
				})

				client, _ = New(server.Url, HTTPOptions{
					Headers: fakeHeaders,
				})
				_, err = client.Get(ctx)
			})

			It("includes headers in the request", func() {
				Expect(err).ToNot(HaveOccurred(), "Server didn't see the headers we were supposed to send")
			})
		})

		When("Creating with backoff", func() {
			var err error
			var retryCount int
			var server *tests.MockServer

			backoffParams := backoff.NewExponentialBackOff()
			backoffParams.MaxInterval = 5 * time.Minute
			backoffParams.MaxElapsedTime = time.Hour

			handleGet := func(w http.ResponseWriter, r *http.Request) {
				if retryCount != 0 {
					retryCount--
					w.WriteHeader(http.StatusBadRequest)
				} else {
					w.WriteHeader(http.StatusOK)
				}
			}

			BeforeEach(func() {
				server = tests.NewMockServer(tests.MockHandler{
					Endpoint:    "/",
					HandlerFunc: handleGet,
				})

				retryCount = 3
				client, err = New(server.Url, HTTPOptions{
					ExponentialBackoff: backoffParams,
				})
			})

			AfterEach(func() {
				server.Close()
			})

			It("it builds without error", func() {
				Expect(err).ToNot(HaveOccurred(), "Client failed to build: %s", err)
			})

			It("retries until it gets a successful http response", func() {
				done := make(chan struct{})
				go func() {
					_, err = client.Get(ctx)
					close(done)
				}()
				Eventually(done, 5*time.Second).Should(BeClosed())
				Expect(err).ToNot(HaveOccurred(), "Client failed to execute a GET request: %s", err)
				Expect(retryCount).To(Equal(0))
			})
		})
	})

	Context("Post", func() {
		When("Sending a POST request without backoff", func() {
			var err error

			handlePost := func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					w.WriteHeader(http.StatusOK)
				} else {
					w.WriteHeader(http.StatusBadRequest)
				}
			}

			BeforeEach(func() {
				server = tests.NewMockServer(tests.MockHandler{
					Endpoint:    "/",
					HandlerFunc: handlePost,
				})

				client, _ = New(server.Url, HTTPOptions{})
				_, err = client.Post(ctx)
			})

			AfterEach(func() {
				server.Close()
			})

			It("sets the method to POST", func() {
				Expect(err).ToNot(HaveOccurred(), "Client failed to execute a POST request: %s", err)
			})
		})
	})

	Context("Patch", func() {
		When("Sending a PATCH request without backoff", func() {
			var err error

			handlePatch := func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPatch {
					w.WriteHeader(http.StatusOK)
				} else {
					w.WriteHeader(http.StatusBadRequest)
				}
			}

			BeforeEach(func() {
				server = tests.NewMockServer(tests.MockHandler{
					Endpoint:    "/",
					HandlerFunc: handlePatch,
				})

				client, _ = New(server.Url, HTTPOptions{})
				_, err = client.Patch(ctx)
			})

			AfterEach(func() {
				server.Close()
			})

			It("sets the method to PATCH", func() {
				Expect(err).ToNot(HaveOccurred(), "Client failed to execute a PATCH request: %s", err)
			})
		})
	})

	Context("Get", func() {
		When("Sending a GET request without backoff", func() {
			var err error

			handleGet := func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(http.StatusOK)
				} else {
					w.WriteHeader(http.StatusBadRequest)
				}
			}

			BeforeEach(func() {
				server = tests.NewMockServer(tests.MockHandler{
					Endpoint:    "/",
					HandlerFunc: handleGet,
				})

				client, _ = New(server.Url, HTTPOptions{})
				_, err = client.Get(ctx)
			})

			AfterEach(func() {
				server.Close()
			})

			It("sets the method to GET", func() {
				Expect(err).ToNot(HaveOccurred(), "Client failed to execute a GET request: %s", err)
			})
		})
	})

	Context("Context", func() {
		When("Cancelling a get request before completion", func() {
			var err error

			delayed := func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-r.Context().Done():
					w.WriteHeader(http.StatusOK)
				case <-time.After(2 * time.Second):
					w.WriteHeader(http.StatusBadRequest)
				}
			}

			BeforeEach(func() {
				server = tests.NewMockServer(tests.MockHandler{
					Endpoint:    "/",
					HandlerFunc: delayed,
				})

				newctx, cancel := context.WithCancel(ctx)
				client, _ = New(server.Url, HTTPOptions{})

				err = fmt.Errorf("context not cancelled yet")
				go func() { _, err = client.Get(newctx) }()
				cancel()
			})

			AfterEach(func() {
				server.Close()
			})

			It("cancels the request immediately", func() {
				time.Sleep(time.Second)
				Expect(err.Error()).To(ContainSubstring("context canceled"), "HTTP request failed to be cancelled!")
			})
		})
	})
})
