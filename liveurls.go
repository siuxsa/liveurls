package main

import (
	"bufio"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type statusResult struct {
	url        string
	statusCode int
}

func checkURL(url string, outputChan chan<- statusResult, verbose bool, wg *sync.WaitGroup) {
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

	outputChan <- statusResult{url: url, statusCode: resp.StatusCode}
}

func processURLs(urls []string, outputChan chan statusResult, requestsPerSecond int, verbose bool, stopChan chan struct{}) {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, requestsPerSecond)
	ticker := time.NewTicker(time.Second / time.Duration(requestsPerSecond))
	defer ticker.Stop()

	for _, url := range urls {
		select {
		case <-stopChan:
			return // Exit if stop signal received
		case <-ticker.C:
			semaphore <- struct{}{} // Acquire semaphore slot
			wg.Add(1)
			go func(u string) {
				defer func() { <-semaphore }() // Release semaphore slot
				checkURL(u, outputChan, verbose, &wg)
			}(url)
		}
	}
	wg.Wait()
}

func saveURLs(filename string, urls []string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("error creating output file %s: %v", filename, err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, url := range urls {
		_, err := writer.WriteString(url + "\n")
		if err != nil {
			return fmt.Errorf("error writing to output file %s: %v", filename, err)
		}
	}
	return writer.Flush()
}

func parseStatusRanges(only string) map[int]bool {
	ranges := make(map[int]bool)
	if only == "" {
		return nil // No specific ranges, use default behavior
	}

	parts := strings.Split(only, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len(part) < 3 || !strings.HasSuffix(part, "xx") {
			continue
		}
		base, err := strconv.Atoi(part[:1])
		if err != nil {
			continue
		}
		for i := 0; i < 100; i++ {
			ranges[base*100+i] = true
		}
	}
	return ranges
}

func main() {
	// Define command-line flags
	listPtr := flag.String("l", "", "File containing list of URLs")
	outputPtr := flag.String("o", "status", "Base name for output files")
	ratePtr := flag.Int("d", 10, "Number of requests per second")
	verbosePtr := flag.Bool("v", false, "Enable verbose output")
	onlyPtr := flag.String("only", "", "Comma-separated status code ranges (e.g., 2xx,3xx)")
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
		fmt.Println("No URLs provided. Usage: liveurls [-l <file>] [-o <output>] [-d <rate>] [-v] [--only <ranges>]")
		os.Exit(1)
	}

	// Parse status code ranges
	statusRanges := parseStatusRanges(*onlyPtr)

	// Channels for URLs and shutdown
	outputChan := make(chan statusResult, len(urls))
	stopChan := make(chan struct{})
	results := make(map[int][]string) // Map of status code to URLs
	var mu sync.Mutex

	// Handle Ctrl+C for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Process URLs in a goroutine
	go func() {
		processURLs(urls, outputChan, *ratePtr, *verbosePtr, stopChan)
		close(outputChan)
	}()

	// Collect results
	go func() {
		for result := range outputChan {
			mu.Lock()
			results[result.statusCode] = append(results[result.statusCode], result.url)
			mu.Unlock()
		}
	}()

	// Wait for either completion or interrupt
	select {
	case <-sigChan:
		close(stopChan)
		fmt.Println("\nReceived interrupt, saving current progress...")
	case <-func() chan struct{} {
		done := make(chan struct{})
		go func() {
			for range outputChan {
			}
			close(done)
		}()
		return done
	}():
	}

	// Save results based on --only or default behavior
	if statusRanges != nil {
		// Specific ranges specified
		var filteredURLs []string
		for status, urls := range results {
			if statusRanges[status] {
				filteredURLs = append(filteredURLs, urls...)
			}
		}
		if err := saveURLs(*outputPtr+".txt", filteredURLs); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Found %d URLs matching %s. Results saved to %s.txt (rate: %d req/s)\n", len(filteredURLs), *onlyPtr, *outputPtr, *ratePtr)
	} else {
		// Default behavior: save to separate files by status code range
		for status, urls := range results {
			rangeFile := fmt.Sprintf("%s_%dxx.txt", *outputPtr, status/100)
			if err := saveURLs(rangeFile, urls); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Found %d URLs with %dxx status. Saved to %s (rate: %d req/s)\n", len(urls), status/100, rangeFile, *ratePtr)
		}
		if len(results) == 0 {
			fmt.Printf("No URLs processed successfully (rate: %d req/s)\n", *ratePtr)
		}
	}
}