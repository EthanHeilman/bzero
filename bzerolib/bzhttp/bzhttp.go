package bzhttp

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"bastionzero.com/bzerolib/logger"
	backoff "github.com/cenkalti/backoff/v4"
)

type bzhttp struct {
	logger        *logger.Logger
	endpoint      string
	contentType   string
	body          []byte
	headers       map[string]string
	params        map[string]string
	backoffParams backoff.BackOff
}

func BuildEndpoint(base string, toAdd string) (string, error) {
	urlObject, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	urlObject.Path = path.Join(urlObject.Path, toAdd)

	// Now undo any url encoding that might have happened in UrlObject.String()
	decodedUrl, err := url.QueryUnescape(urlObject.String())
	if err != nil {
		return "", err
	}

	// There is a problem with path.Join where it interally calls a Clean(..) function
	// which will remove any trailing slashes, this causes issues when proxying requests
	// that are expecting the trailing slash.
	// Ref: https://forum.golangbridge.org/t/how-to-concatenate-paths-for-api-request/5791
	if strings.HasSuffix(toAdd, "/") && !strings.HasSuffix(decodedUrl, "/") {
		decodedUrl += "/"
	}

	return decodedUrl, nil
}

// Helper function to extract the body of a http request
func GetBodyBytes(body io.ReadCloser) ([]byte, error) {
	bodyInBytes, err := io.ReadAll(body)
	if err != nil {
		rerr := fmt.Errorf("error building body: %s", err)
		return nil, rerr
	}
	return bodyInBytes, nil
}

// Helper function to extract headers from a http request
func GetHeaders(headers http.Header) map[string][]string {
	toReturn := make(map[string][]string)
	for name, values := range headers {
		toReturn[name] = values
	}
	return toReturn
}

func Post(logger *logger.Logger, endpoint string, contentType string, body []byte, headers map[string]string, params map[string]string) (*http.Response, error) {
	req := createBzhttp(logger, endpoint, contentType, headers, params, body, defaultBackoffParams())
	return req.post()
}

func Get(logger *logger.Logger, endpoint string, headers map[string]string, params map[string]string) (*http.Response, error) {
	req := createBzhttp(logger, endpoint, "", headers, params, []byte{}, defaultBackoffParams())
	return req.get()
}

func createBzhttp(logger *logger.Logger, endpoint string, contentType string, headers map[string]string, params map[string]string, body []byte, backoffParams *backoff.ExponentialBackOff) bzhttp {
	return bzhttp{
		logger:        logger,
		endpoint:      endpoint,
		contentType:   contentType,
		body:          body,
		headers:       headers,
		params:        params,
		backoffParams: backoffParams,
	}
}

func (b *bzhttp) post() (*http.Response, error) {
	// Default params
	// Ref: https://github.com/cenkalti/backoff/blob/a78d3804c2c84f0a3178648138442c9b07665bda/exponential.go#L76
	// DefaultInitialInterval     = 500 * time.Millisecond
	// DefaultRandomizationFactor = 0.5
	// DefaultMultiplier          = 1.5
	// DefaultMaxInterval         = 60 * time.Second
	// DefaultMaxElapsedTime      = 15 * time.Minute

	// Make our ticker
	ticker := backoff.NewTicker(b.backoffParams)

	// Keep looping through our ticker, waiting for it to tell us when to retry
	for range ticker.C {
		// Make our Client
		var httpClient = getHttpClient()

		// declare our variables
		var response *http.Response
		var err error

		if len(b.headers) == 0 && len(b.params) == 0 {
			response, err = httpClient.Post(b.endpoint, b.contentType, bytes.NewBuffer(b.body))
		} else {
			// Make our Request
			req, _ := http.NewRequest("POST", b.endpoint, bytes.NewBuffer(b.body))
			req = addHeaders(req, b.headers, b.contentType)
			req = addQueryParams(req, b.params)

			response, err = httpClient.Do(req)
		}

		if err != nil {
			b.logger.Errorf("error making POST request: %s", err)
			continue
		} else if err := checkBadStatusCode(response); err != nil {
			ticker.Stop()
			return response, err
		} else if response.StatusCode >= 200 && response.StatusCode < 300 {
			ticker.Stop()
			return response, nil
		} else {
			b.logger.Errorf("Received status code %d making POST request, will retry in %s", response.StatusCode, b.backoffParams.NextBackOff().Round(time.Second))
			continue
		}
	}

	return nil, errors.New("unable to make post request")
}

func (b *bzhttp) get() (*http.Response, error) {
	// Default params
	// Ref: https://github.com/cenkalti/backoff/blob/a78d3804c2c84f0a3178648138442c9b07665bda/exponential.go#L76
	// DefaultInitialInterval     = 500 * time.Millisecond
	// DefaultRandomizationFactor = 0.5
	// DefaultMultiplier          = 1.5
	// DefaultMaxInterval         = 60 * time.Second
	// DefaultMaxElapsedTime      = 15 * time.Minute

	// Make our ticker
	ticker := backoff.NewTicker(b.backoffParams)

	// Keep looping through our ticker, waiting for it to tell us when to retry
	for range ticker.C {
		// Make our Client
		var httpClient = getHttpClient()

		// declare our variables
		var response *http.Response
		var err error

		if len(b.headers) == 0 && len(b.params) == 0 {
			response, err = httpClient.Get(b.endpoint)
		} else {
			// Make our Request
			req, _ := http.NewRequest("GET", b.endpoint, bytes.NewBuffer(b.body))
			req = addHeaders(req, b.headers, b.contentType)
			req = addQueryParams(req, b.params)

			response, err = httpClient.Do(req)
		}

		if err != nil {
			b.logger.Errorf("error making GET request: %s", err)
			continue
		} else if err := checkBadStatusCode(response); err != nil {
			ticker.Stop()
			return response, err
		} else if response.StatusCode >= 200 && response.StatusCode < 300 {
			ticker.Stop()
			return response, nil
		} else {
			b.logger.Errorf("Received status code %d making GET request to %s, will retry in %s", response.StatusCode, b.endpoint, b.backoffParams.NextBackOff().Round(time.Second))
			continue
		}
	}

	return nil, errors.New("unable to make get request")
}

// Helper function to check if we received a status code that we should not attempt to try again
func checkBadStatusCode(response *http.Response) error {
	if response.StatusCode == http.StatusUnauthorized ||
		response.StatusCode == http.StatusUnsupportedMediaType ||
		response.StatusCode == http.StatusGone {
		return fmt.Errorf("received response code: %d, not retrying", response.StatusCode)
	}
	return nil
}

// Helper function to add headers and set the content type
func addHeaders(request *http.Request, headers map[string]string, contentType string) *http.Request {
	// Add the expected headers
	for name, values := range headers {
		// Loop over all values for the name.
		request.Header.Set(name, values)
	}

	// Add the content type header
	request.Header.Set("Content-Type", contentType)

	return request
}

// Helper function to add query params
func addQueryParams(request *http.Request, params map[string]string) *http.Request {
	// Set any query params
	q := request.URL.Query()
	for key, values := range params {
		q.Add(key, values)
	}

	// Add the client protocol for signalr
	q.Add("clientProtocol", "1.5")
	request.URL.RawQuery = q.Encode()

	return request
}

// Helper function to build a http client
func getHttpClient() *http.Client {
	return &http.Client{
		Timeout: time.Second * 30,
	}
}

func defaultBackoffParams() *backoff.ExponentialBackOff {
	// Define our exponential backoff params
	backoffParams := backoff.NewExponentialBackOff()
	backoffParams.MaxElapsedTime = 72 * time.Hour
	backoffParams.MaxInterval = 15 * time.Minute
	return backoffParams
}
