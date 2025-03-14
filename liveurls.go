package main

import (
	"bufio"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

func checkURL(url string, outputChan chan<- string, verbose bool, wg *sync.WaitGroup) {
	defer wg.Done()

	// Add http:// prefix if protocol is missing
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}

	// Make HEAD request to check status code
	resp, err := http.Head(url)
	if verbose {
		if err != nil {
			fmt.Printf("[ERROR] %s: %v\n", url, err)
			return
		}
		fmt.Printf("[CHECK] %s: %d\n", url, resp.StatusCode)
	}

	if err != nil {
		return // Silently skip errors if not verbose
	}
	defer resp.Body.Close()

	// Check if status code is 200
	if resp.StatusCode == http.StatusOK {
		outputChan <- url
	}
}

func processURLs(urls []string, outputChan chan string, requestsPerSecond int, verbose bool) {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, requestsPerSecond)
	ticker := time.NewTicker(time.Second / time.Duration(requestsPerSecond))
	defer ticker.Stop()

	for _, url := range urls {
		// Wait for ticker to allow next request
		<-ticker.C
		semaphore <- struct{}{} // Acquire semaphore slot

		wg.Add(1)
		go func(u string) {
			defer func() { <-semaphore }() // Release semaphore slot
			checkURL(u, outputChan, verbose, &wg)
		}(url)
	}

	wg.Wait()
}

func main() {
	// Define command-line flags
	listPtr := flag.String("l", "", "File containing list of URLs")
	outputPtr := flag.String("o", "live_urls.txt", "Output file for live URLs")
	ratePtr := flag.Int("d", 10, "Number of requests per second")
	verbosePtr := flag.Bool("v", false, "Enable verbose output")
	flag.Parse()

	var urls []string

	// Check if reading from file (-l flag)
	if *listPtr != "" {
		file, err := os.Open(*listPtr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening file: %v\n", err)
			os.Exit(1)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			url := strings.TrimSpace(scanner.Text())
			if url != "" {
				urls = append(urls, url)
			}
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Read from stdin if no file specified
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			url := strings.TrimSpace(scanner.Text())
			if url != "" {
				urls = append(urls, url)
			}
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
			os.Exit(1)
		}
	}

	if len(urls) == 0 {
		fmt.Println("No URLs provided. Usage: liveurls -l <file> -o <output> -d <rate> -v or cat urls.txt | liveurls -o <output> -d <rate> -v")
		os.Exit(1)
	}

	// Create output channel
	outputChan := make(chan string, len(urls))

	// Process URLs with rate limiting
	go processURLs(urls, outputChan, *ratePtr, *verbosePtr)

	// Collect live URLs
	var liveURLs []string
	for url := range outputChan {
		liveURLs = append(liveURLs, url)
	}

	// Close output channel when all URLs are processed
	close(outputChan)

	// Write results to output file
	outputFile, err := os.Create(*outputPtr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
		os.Exit(1)
	}
	defer outputFile.Close()

	writer := bufio.NewWriter(outputFile)
	for _, url := range liveURLs {
		_, err := writer.WriteString(url + "\n")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing to output file: %v\n", err)
			os.Exit(1)
		}
	}
	writer.Flush()

	if *verbosePtr {
		fmt.Printf("\nProcessed %d URLs, found %d live URLs. Results saved to %s (rate: %d req/s)\n", len(urls), len(liveURLs), *outputPtr, *ratePtr)
	} else {
		fmt.Printf("Found %d live URLs. Results saved to %s (rate: %d req/s)\n", len(liveURLs), *outputPtr, *ratePtr)
	}
}