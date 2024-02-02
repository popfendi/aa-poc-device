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

var (
	offset float64 = 100.0
)

func parseOffset() {
	// Assuming you have an environment variable named "MY_FLOAT_VAR"
	env := os.Getenv("DB_OFFSET")
	if env == "" {
		Sugar.Errorf("Environment variable DB_OFFSET is not set, defaulting to %f", offset)
		return
	}

	// Convert the environment variable to float64
	floatValue, err := strconv.ParseFloat(env, 64)
	if err != nil {
		Sugar.Errorf("Error converting %s to float64: %v\n", env, err)
		return
	}

	offset = floatValue
	Sugar.Infof("dB offset set to %f\n", floatValue)
}

func analyze(dataChannel chan<- []byte) {
	parseOffset()
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
		frequencyBands := []float64{20, 25, 30, 31.5, 35, 40, 45, 50, 55, 63, 70, 80, 90, 100, 110, 125, 140, 160, 180, 200, 225, 250, 280, 315, 360, 400, 450, 500, 565, 630, 715, 800, 900, 1000, 1125, 1250, 1375, 1500, 1750, 2000, 2250, 2500, 2825, 3150, 3575, 4000, 4500, 5000, 5650, 6300, 7150, 8000, 10000, 12000, 14000, 16000, 18000, 20000, 22000}

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
					obj[strconv.FormatFloat(frequencyBands[bandEnd], 'f', -1, 64)] = convertToCalibratedDB(bandMaxDB, offset)
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
			obj[strconv.FormatFloat(frequencyBands[bandEnd], 'f', -1, 64)] = convertToCalibratedDB(bandMaxDB, offset)
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

func convertToCalibratedDB(dBFSValue float64, calibrationOffset float64) float64 {
	return calibrationOffset - dBFSValue
}
