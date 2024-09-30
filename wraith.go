package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"math/rand"

	"github.com/gocolly/colly/v2"
)

type Result struct {
	Source string
	URL    string
	Where  string
}

var headers map[string]string
var sm sync.Map // Thread-safe map


func getRandomUserAgent() string {
	userAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.3",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/51.0.2704.103 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_11_5) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/50.0.2661.102 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:54.0) Gecko/20100101 Firefox/54.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:80.0) Gecko/20100101 Firefox/80.0",
	}
	rand.Seed(time.Now().UnixNano())
	return userAgents[rand.Intn(len(userAgents))]
}

func main() {
	inside := flag.Bool("i", false, "Only crawl inside path")
	threads := flag.Int("t", 16, "Number of threads to utilize.")
	depth := flag.Int("d", 4, "Depth to crawl.")
	maxSize := flag.Int("size", -1, "Page size limit, in KB.")
	insecure := flag.Bool("insecure", false, "Disable TLS verification.")
	subsInScope := flag.Bool("subs", false, "Include subdomains for crawling.")
	showJson := flag.Bool("json", false, "Output as JSON.")
	showSource := flag.Bool("s", false, "Show the source of URL based on where it was found. E.g. href, form, script, etc.")
	showWhere := flag.Bool("w", false, "Show at which link the URL is found.")
	rawHeaders := flag.String("h", "", "Custom headers separated by two semi-colons. E.g. -h \"Cookie: foo=bar;;Referer: http://example.com/\" ")
	unique := flag.Bool("u", false, "Show only unique URLs.")
	proxy := flag.String("proxy", "", "Proxy URL. E.g. -proxy http://127.0.0.1:8080")
	timeout := flag.Int("timeout", 360, "Maximum time to crawl each URL from stdin, in seconds.")
	disableRedirects := flag.Bool("dr", false, "Disable following HTTP redirects.")
	crawlJS := flag.Bool("crawl-js", false, "Crawl for URLs inside JavaScript files.")
	wayback := flag.Bool("wayback", false, "Fetch URLs from Wayback Machine and crawl them.")

	flag.Parse()

	if *proxy != "" {
		os.Setenv("PROXY", *proxy)
	}
	proxyURL, _ := url.Parse(os.Getenv("PROXY"))

	err := parseHeaders(*rawHeaders)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error parsing headers:", err)
		os.Exit(1)
	}

	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		fmt.Fprintln(os.Stderr, "No URLs detected. Hint: cat urls.txt | wraith")
		os.Exit(1)
	}

	results := make(chan string, *threads)
	go func() {
		s := bufio.NewScanner(os.Stdin)
		for s.Scan() {
			urlText := s.Text()
			hostname, err := extractHostname(urlText)
			if err != nil {
				log.Println("Error parsing URL:", err)
				continue
			}

			allowedDomains := []string{hostname}
			if headers != nil {
				if val, ok := headers["Host"]; ok {
					allowedDomains = append(allowedDomains, val)
				}
			}

			c := colly.NewCollector(
				colly.UserAgent(getRandomUserAgent()),
				colly.AllowedDomains(allowedDomains...),
				colly.MaxDepth(*depth),
				colly.Async(true),
			)


			if *maxSize != -1 {
				c.MaxBodySize = *maxSize * 1024
			}

			if *subsInScope {
				c.AllowedDomains = nil
				c.URLFilters = []*regexp.Regexp{regexp.MustCompile(".*(\\.|\\/\\/)" + strings.ReplaceAll(hostname, ".", "\\.") + "((#|\\/|\\?).*)?")}
			}

			if *disableRedirects {
				c.SetRedirectHandler(func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				})
			}

			if *wayback {
				tempFile := "wayback_urls.txt"
				hostname, _ := extractHostname(urlText)

				// Fetch URLs from Wayback Machine and save them to a file
				err := fetchWaybackURLs(hostname, tempFile)
				if err != nil {
					log.Fatalf("Error fetching Wayback URLs: %v", err)
				}

				// Open the temporary file and scan through the URLs
				waybackFile, err := os.Open(tempFile)
				if err != nil {
					log.Fatalf("Failed to open wayback temp file: %v", err)
				}
				defer waybackFile.Close()

				waybackScanner := bufio.NewScanner(waybackFile)
				for waybackScanner.Scan() {
					urlText := waybackScanner.Text()
					results <- urlText // Append Wayback URLs to the crawling process
				}

				if err := waybackScanner.Err(); err != nil {
					log.Fatalf("Error reading wayback URLs: %v", err)
				}

				// Clean up the temporary file
				os.Remove(tempFile)
			}



			c.Limit(&colly.LimitRule{DomainGlob: "*", Parallelism: *threads})

			c.OnHTML("a[href]", func(e *colly.HTMLElement) {
				link := e.Attr("href")
				absLink := e.Request.AbsoluteURL(link)
				if strings.Contains(absLink, urlText) || !*inside {
					printResult(link, "href", *showSource, *showWhere, *showJson, results, e)
					e.Request.Visit(link)
				}
			})

			c.OnHTML("script[src]", func(e *colly.HTMLElement) {
				printResult(e.Attr("src"), "script", *showSource, *showWhere, *showJson, results, e)
				if *crawlJS {
					e.Request.Visit(e.Attr("src"))
				}
			})

			c.OnHTML("form[action]", func(e *colly.HTMLElement) {
				printResult(e.Attr("action"), "form", *showSource, *showWhere, *showJson, results, e)
			})

			c.OnRequest(func(r *colly.Request) {
				for header, value := range headers {
					r.Headers.Set(header, value)
				}
			})
			c.OnResponse(func(r *colly.Response) {
				if *crawlJS && strings.HasSuffix(r.Request.URL.Path, ".js") {
					jsContent := string(r.Body)
					re := regexp.MustCompile(`https?:\/\/[^\s'"<>]+`)
					foundLinks := re.FindAllString(jsContent, -1)

					for _, link := range foundLinks {
						absLink := r.Request.AbsoluteURL(link)
						if strings.Contains(absLink, r.Request.URL.Hostname()) {
						    printResult(absLink, "js", *showSource, *showWhere, *showJson, results, nil)
						}
					}

					// Print .js file in output without logging verbose info
					printResult(r.Request.URL.String(), "js-file", *showSource, *showWhere, *showJson, results, nil)
				}
			})




			if *proxy != "" {
				c.WithTransport(&http.Transport{
					Proxy:           http.ProxyURL(proxyURL),
					TLSClientConfig: &tls.Config{InsecureSkipVerify: *insecure},
				})
			} else {
				c.WithTransport(&http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: *insecure},
				})
			}

			// Auto-add http or https if not provided
			finalURL, err := addSchemeIfMissing(urlText)
			if err != nil {
				log.Println("[Invalid URL]:", urlText, "Skipping...")
				continue
			}

			if *timeout == -1 {
				c.Visit(finalURL)
				c.Wait()
			} else {
				finished := make(chan int, 1)
				go func() {
					c.Visit(finalURL)
					c.Wait()
					finished <- 0
				}()
				select {
				case <-finished:
					close(finished)
				case <-time.After(time.Duration(*timeout) * time.Second):
					log.Println("[timeout]", finalURL)
				}
			}
		}

		if err := s.Err(); err != nil {
			fmt.Fprintln(os.Stderr, "reading standard input:", err)
		}
		close(results)
	}()

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	if *unique {
		for res := range results {
			if isUnique(res) {
				fmt.Fprintln(w, res)
			}
		}
	}
	for res := range results {
		fmt.Fprintln(w, res)
	}
}

func fetchWaybackURLs(domain string, filename string) error {
    apiURL := fmt.Sprintf("http://web.archive.org/cdx/search/cdx?url=*.%s&collapse=urlkey&filter=statuscode:200", domain)

    // Fetch data from Wayback Machine API
    resp, err := http.Get(apiURL)
    if err != nil {
        return fmt.Errorf("failed to fetch Wayback URLs: %v", err)
    }
    defer resp.Body.Close()

    // Create or overwrite the temporary file
    file, err := os.Create(filename)
    if err != nil {
        return fmt.Errorf("failed to create temp file: %v", err)
    }
    defer file.Close()

    scanner := bufio.NewScanner(resp.Body)
    for scanner.Scan() {
        fields := strings.Fields(scanner.Text())
        if len(fields) > 1 {
            // Write each URL to the temp file
            file.WriteString(fmt.Sprintf("http://%s\n", fields[2]))
        }
    }

    return scanner.Err()
}


// parseHeaders does validation of headers input and saves it to a formatted map.
func parseHeaders(rawHeaders string) error {
	if rawHeaders != "" {
		if !strings.Contains(rawHeaders, ":") {
			return errors.New("headers flag not formatted properly (no colon to separate header and value)")
		}

		headers = make(map[string]string)
		rawHeadersArr := strings.Split(rawHeaders, ";;")
		for _, header := range rawHeadersArr {
			parts := strings.SplitN(header, ":", 2)
			headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return nil
}

// extractHostname extracts the hostname from a URL and returns it.
func extractHostname(urlString string) (string, error) {
	u, err := url.Parse(urlString)
	if err != nil || !u.IsAbs() {
		return "", errors.New("Input must be a valid absolute URL")
	}
	return u.Hostname(), nil
}

// addSchemeIfMissing adds "http://" or "https://" if not already present.
func addSchemeIfMissing(rawURL string) (string, error) {
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL, nil
	}

	testURL := "http://" + rawURL
	_, err := http.Get(testURL)
	if err == nil {
		return testURL, nil
	}

	testURL = "https://" + rawURL
	_, err = http.Get(testURL)
	if err == nil {
		return testURL, nil
	}

	return "", fmt.Errorf("unable to validate URL: %s", rawURL)
}

func printResult(link string, sourceName string, showSource bool, showWhere bool, showJson bool, results chan string, e *colly.HTMLElement) {
	if e != nil {
		result := e.Request.AbsoluteURL(link)
		whereURL := e.Request.URL.String()
		if result != "" {
			if showJson {
				r := &Result{
					Source: sourceName,
					URL:    result,
					Where:  whereURL,
				}
				jsonStr, err := json.Marshal(r)
				if err == nil {
					results <- string(jsonStr)
				}
			} else {
				out := result
				if showSource {
					out += " [source: " + sourceName + "]"
				}
				if showWhere {
					out += " [from: " + whereURL + "]"
				}
				results <- out
			}
		}
	}
}

func isUnique(link string) bool {
	_, loaded := sm.LoadOrStore(link, true)
	return !loaded
}
