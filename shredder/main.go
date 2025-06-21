package main

import (
	"C"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

var logFile *os.File

type ProgressCallback func(percentage int)

func SecureShredFile(filePath string, passes int, progressCallback ProgressCallback) (bool, string, error) {
	fileInfo, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return false, "", fmt.Errorf("file does not exist: %s", filePath)
	}
	if err != nil {
		return false, "", fmt.Errorf("error getting file info for %s: %w", filePath, err)
	}

	file, err := os.OpenFile(filePath, os.O_RDWR, 0)
	if err != nil {
		return false, "", fmt.Errorf("no write permission or error opening file %s: %w", filePath, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			log.Printf("Error closing file %s: %v", filePath, closeErr)
		}
	}()

	fileSize := fileInfo.Size()
	chunkSize := int64(64 * 1024) // 64 KB

	// Calculate number of chunks. Ensure at least one chunk for empty files.
	numChunks := int64(1)
	if fileSize > 0 {
		numChunks = int64(math.Ceil(float64(fileSize) / float64(chunkSize)))
	}

	totalSteps := int64(passes)*numChunks + numChunks
	currentStep := int64(0)

	for i := 0; i < passes; i++ {
		_, err = file.Seek(0, io.SeekStart)
		if err != nil {
			return false, "", fmt.Errorf("error seeking file %s: %w", filePath, err)
		}

		for j := int64(0); j < numChunks; j++ {
			bufferSize := chunkSize
			if j == numChunks-1 && fileSize%chunkSize != 0 {
				bufferSize = fileSize % chunkSize
			}
			if bufferSize == 0 { // Handle case where file size is 0 or perfectly divisible
				bufferSize = chunkSize
			}

			data := make([]byte, bufferSize)
			_, err := rand.Read(data)
			if err != nil { // redundant lol
				return false, "", fmt.Errorf("error generating random data for %s: %w", filePath, err)
			}

			_, err = file.Write(data)
			if err != nil {
				return false, "", fmt.Errorf("error writing random data to %s: %w", filePath, err)
			}
			err = file.Sync() // Ensure data is written to disk
			if err != nil {
				return false, "", fmt.Errorf("error syncing file %s after writing random data: %w", filePath, err)
			}

			currentStep++
			if progressCallback != nil {
				progressCallback(int(float64(currentStep) / float64(totalSteps) * 100))
			}
		}
	}

	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return false, "", fmt.Errorf("error seeking file %s before zeroing: %w", filePath, err)
	}

	zeroBuffer := make([]byte, chunkSize)
	for j := int64(0); j < numChunks; j++ {
		bufferSize := chunkSize
		if j == numChunks-1 && fileSize%chunkSize != 0 {
			bufferSize = fileSize % chunkSize
		}
		if bufferSize == 0 {
			bufferSize = chunkSize
		}

		_, err = file.Write(zeroBuffer[:bufferSize])
		if err != nil {
			return false, "", fmt.Errorf("error writing zeros to %s: %w", filePath, err)
		}
		err = file.Sync()
		if err != nil {
			return false, "", fmt.Errorf("error syncing file %s after writing zeros: %w", filePath, err)
		}

		currentStep++
		if progressCallback != nil {
			progressCallback(int(float64(currentStep) / float64(totalSteps) * 100))
		}
	}

	if closeErr := file.Close(); closeErr != nil {
		return false, "", fmt.Errorf("error closing file %s before final operations: %w", filePath, closeErr)
	}

	err = os.Truncate(filePath, 0)
	if err != nil {
		return false, "", fmt.Errorf("error truncating file %s: %w", filePath, err)
	}
	// Note: os.Truncate implicitly syncs, but some systems might benefit from explicit sync on parent directory

	dir := filepath.Dir(filePath)
	randomBytes := make([]byte, 32)
	_, err = rand.Read(randomBytes)
	if err != nil {
		return false, "", fmt.Errorf("error generating random bytes for new file name: %w", err)
	}
	randomSuffix := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(randomBytes)

	timestampStr := strconv.FormatInt(time.Now().Unix(), 10)
	timestampEncoded := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(timestampStr))

	newFileName := fmt.Sprintf("%s_%s", timestampEncoded, randomSuffix)
	newPath := filepath.Join(dir, newFileName)

	err = os.Rename(filePath, newPath)
	if err != nil {
		return false, "", fmt.Errorf("error renaming file from %s to %s: %w", filePath, newPath, err)
	}

	log.Printf("Redacted: %s -> Renamed to: %s", filePath, newPath)

	if progressCallback != nil {
		progressCallback(100)
	}

	return true, newPath, nil
}

//export shred
func shred(path *C.char, count C.int) {
	filePath := C.GoString(path)
	passes := int(count)

	progressCallback := func(percentage int) {
		log.Printf("\rShredding progress: %d%%", percentage)
	}

	log.Println("\nStarting file shredding...")
	success, newPath, shredErr := SecureShredFile(filePath, passes, progressCallback)
	log.Println()

	if shredErr != nil {
		log.Fatalf("File shredding failed: %v", shredErr)
	}

	if success {
		log.Printf("File shredded successfully! Original: %s, Renamed to: %s\n", filePath, newPath)
		if _, err := os.Stat(filePath); !os.IsNotExist(err) {
			log.Printf("Warning: Original file '%s' still exists after shredding.\n", filePath)
		} else {
			log.Printf("Original file '%s' no longer exists.\n", filePath)
		}
	} else {
		log.Printf("File shredding failed for %s. New path: %s\n", filePath, newPath)
	}

	if success {
		if err := os.Remove(newPath); err != nil {
			log.Printf("Error removing renamed file %s: %v", newPath, err)
		} else {
			log.Printf("Cleaned up renamed file '%s'.\n", newPath)
		}
	}
}

//export init_logger
func init_logger() {
	logFile, err := os.OpenFile("shredder.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	log.SetOutput(logFile)
}

//export close_logger
func close_logger() {
	if logFile != nil {
		logFile.Close()
	}
}

// required for compiling
func main() {}
