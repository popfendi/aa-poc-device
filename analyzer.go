package main

import (
	"encoding/json"
	"math"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/mjibson/go-dsp/spectral"
)

var buffer []float32
var mu sync.Mutex

func analyze(dataChannel chan<- []byte) {
	processAudio := func(in []float32) {
		mu.Lock()
		defer mu.Unlock()

		// Append captured audio to buffer
		buffer = append(buffer, in...)

		const sampleRate = 44100
		const fftSize = 1024
		const bufferSize = 22050

		// Ensure we have enough data before proceeding
		if len(buffer) < bufferSize {
			return
		}

		// Convert to float64 for spectral analysis
		data := make([]float64, len(buffer))
		for i, v := range buffer {
			data[i] = float64(v)
		}

		// Calculate power spectral density
		psd, freqs := spectral.Pwelch(data, sampleRate, &spectral.PwelchOptions{NFFT: fftSize})

		// Define the frequency bands
		frequencyBands := []float64{20, 25, 31.5, 40, 50, 63, 80, 100, 125, 160, 200, 250, 315, 400, 500, 630, 800, 1000, 1250, 1500, 2000, 2500, 3150, 4000, 5000, 6300, 8000, 12000, 16000, 20000, 22000, 22050}

		// Initialize variables to store band information

		bandEnd := 0
		bandMaxDB := -math.Inf(1) // Initialize to negative infinity

		obj := make(map[string]float64)

		// Iterate through the PSD and frequency values
		for i, p := range psd {
			freq := freqs[i]
			db := 10 * math.Log10(p)

			// Check if the current frequency is within the current band
			if freq <= frequencyBands[bandEnd] {
				// Update the maximum DB value if the current DB is greater
				if db > bandMaxDB {
					bandMaxDB = db

				}
			} else {
				// Store the maximum DB value for the previous band
				if bandMaxDB > -math.Inf(1) {
					obj[strconv.FormatFloat(frequencyBands[bandEnd], 'f', -1, 64)] = bandMaxDB
				}

				// Update the band indices
				bandEnd++

				// Reset the maximum DB value
				bandMaxDB = -math.Inf(1)

				// Check if the current frequency is within the new band
				if freq <= frequencyBands[bandEnd] {
					// Update the maximum DB value
					bandMaxDB = db

				}
			}
		}

		// Store the maximum DB value for the last band
		if bandMaxDB > -math.Inf(1) {
			obj[strconv.FormatFloat(frequencyBands[bandEnd], 'f', -1, 64)] = bandMaxDB
		}

		output := make(map[string]interface{})

		ts := strconv.FormatInt(time.Now().UnixMilli(), 10)

		output["spectrum"] = obj
		output["ts"] = ts

		// Convert output to JSON
		jsonOutput, err := json.Marshal(output)
		if err != nil {
			Sugar.Errorf("Error converting output to JSON: %s", err.Error())
			return
		}

		dataChannel <- jsonOutput

		// Clear the buffer
		buffer = nil
	}

	// Initialize PortAudio
	portaudio.Initialize()
	defer portaudio.Terminate()

	// Set up microphone input stream
	stream, err := portaudio.OpenDefaultStream(1, 0, 44100, 1024, processAudio)
	if err != nil {
		Sugar.Fatalf("Failed to open audio stream: %s", err.Error())
	}
	defer stream.Close()

	// Start capturing audio
	if err := stream.Start(); err != nil {
		Sugar.Fatalf("Failed to start audio stream: %s", err.Error())
	}
	defer stream.Stop()

	Sugar.Info("Capturing audio...")

	// Wait for Ctrl+C
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig

	Sugar.Info("Exiting with code 1")
}
