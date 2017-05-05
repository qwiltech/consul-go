package consul

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/segmentio/objconv/json"
)

const (
	// DefaultAddress is the default consul agent address used when creating a
	// consul client.
	DefaultAddress = "localhost:8500"
)

var (
	// DefaultTransport is the default HTTP transport used by consul clients.
	// It differs from the default transport in net/http because we don't want
	// to enable compression, or allow requests to be proxied. The sizes of the
	// connection pool is also tuned to lower numbers since clients usually
	// communicate with their local agent only. Finally the timeouts are set to
	// lower values because the client and agent most likely communicate over
	// the loopback interface.
	DefaultTransport http.RoundTripper = &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 5 * time.Second,
		}).DialContext,
		DisableCompression:    true,
		MaxIdleConns:          5,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 1 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	// DefaultClient is the default client used when none is specified.
	DefaultClient = &Client{}

	// DefaultUserAgent is the default user agent used by consul clients when
	// none has been set.
	DefaultUserAgent string
)

func init() {
	DefaultUserAgent = fmt.Sprintf("%s (github.com/segmentio/consul-go)", filepath.Base(os.Args[0]))
}

// A Client exposes an API for communicating with a consul agent.
//
// The properties of a client are only read by its method, it is therefore safe
// to use a client concurrently from multiple goroutines.
type Client struct {
	// Addr of the consul agent this client sends requests to.
	// If Address is an empty string then DefaultAddress is used instead.
	Address string

	// UserAgent may be set to any string which identify who the client is.
	UserAgent string

	// Datacenter may be set to configure which consul datacenter the client
	// sends requests for.
	// If Datacenter is an empty string the agent's default is used.
	Datacenter string

	// Session is the ID of a session used by the client to acquire locks.
	Session SessionID

	// Transport is the HTTP transport used by the client to send requests to
	// its agent.
	// If Transport is nil then DefaultTransport is used instead.
	Transport http.RoundTripper
}

// Get sends a GET request to the consul agent.
//
// See (*Client).Do for the full documentation.
func (c *Client) Get(ctx context.Context, path string, query Query, recv interface{}) error {
	return c.Do(ctx, "GET", path, query, nil, recv)
}

// Put sends a PUT request to the consul agent.
//
// See (*Client).Do for the full documentation.
func (c *Client) Put(ctx context.Context, path string, query Query, send interface{}, recv interface{}) error {
	return c.Do(ctx, "PUT", path, query, send, recv)
}

// Delete sends a DELETE request to the consul agent.
//
// See (*Client).Do for the full documentation.
func (c *Client) Delete(ctx context.Context, path string, query Query) error {
	return c.Do(ctx, "DELETE", path, query, nil, nil)
}

// Do sends a request to the consul agent. The method, path, and query arguments
// represent the API call being made. The send argument is the value sent in the
// body of the request, which is usually of struct type, or nil if the request
// has an empty body. The recv argument should be a pointer to a type which
// matches the format of the response, or nil if no response is expected.
func (c *Client) Do(ctx context.Context, method string, path string, query Query, send interface{}, recv interface{}) (err error) {
	var scheme = "http"
	var address = c.Address
	var transport = c.Transport
	var userAgent = c.UserAgent

	if len(address) == 0 {
		address = DefaultAddress
	} else if i := strings.Index(address, "://"); i >= 0 {
		scheme, address = address[:i], address[i+3:]
	}

	if len(userAgent) == 0 {
		userAgent = DefaultUserAgent
	}

	if transport == nil {
		transport = DefaultTransport
	}

	if dc := c.Datacenter; len(dc) != 0 {
		query = append(query, Param{"dc", dc})
	}

	var body []byte
	var req *http.Request
	var res *http.Response
	var url = &url.URL{
		Scheme:   scheme,
		Host:     address,
		Path:     path,
		RawQuery: query.String(),
	}

	if send != nil {
		if data, ok := send.([]byte); ok {
			body = data
		} else if body, err = json.Marshal(send); err != nil {
			return
		}
	}

	req = &http.Request{
		Method:     method,
		URL:        url,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header: http.Header{
			"Accept":       {"application/json; charset=utf-8"},
			"Content-Type": {"application/json; charset=utf-8"},
			"Host":         {address},
			"User-Agent":   {userAgent},
		},
		Body:          ioutil.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	if res, err = transport.RoundTrip(req); err != nil {
		return
	}
	defer res.Body.Close()
	defer io.Copy(ioutil.Discard, req.Body)

	if res.StatusCode != http.StatusOK {
		err = fmt.Errorf("%s %s: %s", method, url, res.Status)
	} else if recv != nil {
		err = json.NewDecoder(res.Body).Decode(recv)
	}
	return
}

func (c *Client) checkSession(op string) (err error) {
	if len(c.Session) == 0 {
		err = errors.New(op + " requires a consul session but the client has none")
	}
	return
}

// Query is a representation of a URL query string as a list of parameters.
type Query []Param

// Param represents a single item in a query string.
type Param struct {
	Name  string
	Value string
}

// String satisfies the fmt.Stringer interface.
func (q Query) String() string {
	b := make([]byte, 0, 100)

	for i, p := range q {
		if i != 0 {
			b = append(b, '&')
		}
		b = append(b, url.QueryEscape(p.Name)...)
		if len(p.Value) != 0 {
			b = append(b, '=')
			b = append(b, url.QueryEscape(p.Value)...)
		}
	}

	return string(b)
}

// Values converts q to a url.Values.
func (q Query) Values() url.Values {
	v := make(url.Values, len(q))

	for _, p := range q {
		v.Set(p.Name, p.Value)
	}

	return v
}
