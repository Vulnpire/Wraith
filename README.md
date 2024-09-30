# Wraith

Fast golang web crawler for gathering URLs and JavaScript file locations. This is basically a simple implementation of the awesome Gocolly library.

## Example usages

Single URL:

```
echo https://google.com | wraith
```

Multiple URLs:

```
cat urls.txt | wraith
```

Timeout for each line of stdin after 5 seconds:

```
cat urls.txt | wraith -timeout 5
```

Send all requests through a proxy:

```
cat urls.txt | wraith -proxy http://localhost:8080
```

Include subdomains:

```
echo https://google.com | wraith -subs
```

> Note: a common issue is that the tool returns no URLs. This usually happens when a domain is specified (https://example.com), but it redirects to a subdomain (https://www.example.com). The subdomain is not included in the scope, so the no URLs are printed. In order to overcome this, either specify the final URL in the redirect chain or use the `-subs` option to include subdomains.

## Example tool chain

Get all subdomains of google, find the ones that respond to http(s), crawl them all.

```
echo google.com | subfinder -all -silent | dnsx -r <path/to/resolvers> -silent -threads 300 | httpx -silent -threads 300 -mc 200 | wraith -crawl-js
```

## Installation

### Normal Install

First, you'll need to [install go](https://golang.org/doc/install).

Then run this command to download + compile hakrawler:
```
go install github.com/Vulnpire/wraith@latest
```

You can now run `~/go/bin/wraith`. If you'd like to just run `wraith` without the full path, you'll need to `export PATH="~/go/bin/:$PATH"`. You can also add this line to your `~/.bashrc` file if you'd like this to persist.

Then, to run wraith:

## Command-line options
```
Usage of wraith:
  -crawl-js
        Crawl for URLs inside JavaScript files.
  -d int
        Depth to crawl. (default 4)
  -dr
        Disable following HTTP redirects.
  -h string
        Custom headers separated by two semi-colons. E.g. -h "Cookie: foo=bar;;Referer: http://example.com/" 
  -i    Only crawl inside path
  -insecure
        Disable TLS verification.
  -json
        Output as JSON.
  -proxy string
        Proxy URL. E.g. -proxy http://127.0.0.1:8080
  -s    Show the source of URL based on where it was found. E.g. href, form, script, etc.
  -size int
        Page size limit, in KB. (default -1)
  -subs
        Include subdomains for crawling.
  -t int
        Number of threads to utilize. (default 16)
  -timeout int
        Maximum time to crawl each URL from stdin, in seconds. (default 360)
  -u    Show only unique URLs.
  -w    Show at which link the URL is found.
  -wayback
        Fetch URLs from Wayback Machine and crawl them.
```

## Axiom support

```
[{
    "command":"cat input | /home/op/go/bin/wraith -d 6 -t 16 -timeout 360 | anew output",
    "ext":"txt"
}]
```

![image](https://github.com/user-attachments/assets/8da4b4ed-08f3-440a-95fa-3b42de702c2a)

