# LiveURLs

LiveURLs is a command-line tool written in Go that checks a list of URLs and saves only those returning HTTP status code 200 (OK) to an output file. It supports rate limiting and verbose output, making it suitable for both quick checks and detailed debugging.

## Features
- Checks URLs for HTTP 200 status using HEAD requests
- Supports input from a file or piped stdin (e.g., `cat urls.txt | liveurls`)
- Rate limiting to control requests per second
- Verbose output option for detailed status and error reporting
- Concurrent processing for efficient URL checking
- Automatically adds "http://" prefix if missing from URLs
- Saves live URLs to a specified output file

## Installation
1. Ensure you have Go installed on your system (version 1.16 or later recommended).
2. Clone this repository:
   ```bash
   git clone https://github.com/yourusername/liveurls.git
   cd liveurls

## Build and install
   ```bashgo
  build liveurls.go
  sudo mv liveurls /usr/local/bin/
````
This makes liveurls available system-wide.

## Usage
```bash
liveurls [-l <file>] [-o <output>] [-d <rate>] [-v]
````
## Options

 - `-l` <file> Input file containing URLs (one per line)
 - `-o` <output> Output file for live URLs (default: live_urls.txt)
 - `-d` <rate> Requests per second (default: 10)
 - `-v` Enable verbose output

## Examples
 - Check URLs from a file with default rate:
````bash
liveurls -l urls.txt -o live.txt
````
