package main

import (
	"bufio"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
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

func processURLs(urls []string, outputChan chan string, requestsPerSecond int, verbose bool, stopChan chan struct{}) {
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

func saveURLs(outputFile string, liveURLs []string) error {
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("error creating output file: %v", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, url := range liveURLs {
		_, err := writer.WriteString(url + "\n")
		if err != nil {
			return fmt.Errorf("error writing to output file: %v", err)
		}
	}
	return writer.Flush()
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

	// Channels for URLs and shutdown
	outputChan := make(chan string, len(urls))
	stopChan := make(chan struct{})
	var liveURLs []string
	var mu sync.Mutex

	// Handle Ctrl+C for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Process URLs in a goroutine
	go func() {
		processURLs(urls, outputChan, *ratePtr, *verbosePtr, stopChan)
		close(outputChan) // Close channel when processing is done
	}()

	// Collect URLs and handle shutdown
	go func() {
		for url := range outputChan {
			mu.Lock()
			liveURLs = append(liveURLs, url)
			mu.Unlock()
		}
	}()

	// Wait for either completion or interrupt
	select {
	case <-sigChan:
		close(stopChan) // Signal to stop processing
		fmt.Println("\nReceived interrupt, saving current progress...")
	case <-func() chan struct{} {
		done := make(chan struct{})
		go func() {
			for range outputChan {
			} // Wait for outputChan to close
			close(done)
		}()
		return done
	}():
		// Normal completion
	}

	// Save URLs to file
	if err := saveURLs(*outputPtr, liveURLs); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving URLs: %v\n", err)
		os.Exit(1)
	}

	if *verbosePtr {
		fmt.Printf("\nProcessed %d URLs, found %d live URLs. Results saved to %s (rate: %d req/s)\n", len(urls), len(liveURLs), *outputPtr, *ratePtr)
	} else {
		fmt.Printf("Found %d live URLs. Results saved to %s (rate: %d req/s)\n", len(liveURLs), *outputPtr, *ratePtr)
	}
}