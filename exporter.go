package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/barasher/go-exiftool"
	"gopkg.in/masci/flickr.v3"
	"gopkg.in/masci/flickr.v3/photosets"
)

type FlickrExporter struct {
	client    *flickr.FlickrClient
	outputDir string
	et        *exiftool.Exiftool
	verbose   bool
}

type Photo struct {
	ID          string
	Title       string
	Description string
	Tags        []string
	OriginalURL string
	Filename    string
	DateTaken   time.Time
}

type Album struct {
	ID          string
	Title       string
	Description string
	DateCreated time.Time
	Photos      []Photo
}

type CollectionSet struct {
	ID          string `xml:"id,attr"`
	Title       string `xml:"title,attr"`
	Description string `xml:"description,attr"`
	DateCreate  int    `xml:"date_create,attr"`
	DateUpdate  int    `xml:"date_update,attr"`
}

type CollectionNode struct {
	ID    string          `xml:"id,attr"`
	Title string          `xml:"title,attr"`
	Sets  []CollectionSet `xml:"set"`
}

type CollectionsResponse struct {
	flickr.BasicResponse
	Collections []CollectionNode `xml:"collections>collection"`
}

func NewFlickrExporter(apiKey, apiSecret, oauthToken, oauthTokenSecret, outputDir string, verbose bool) (*FlickrExporter, error) {
	client := flickr.NewFlickrClient(apiKey, apiSecret)

	// If OAuth tokens are provided, set them
	if oauthToken != "" && oauthTokenSecret != "" {
		client.OAuthToken = oauthToken
		client.OAuthTokenSecret = oauthTokenSecret
		fmt.Println("Using provided OAuth tokens for authentication")
	} else {
		return nil, fmt.Errorf("OAuth tokens are required. Please run 'flickr-exporter auth' first to authenticate")
	}

	et, err := exiftool.NewExiftool()
	if err != nil {
		return nil, fmt.Errorf("could not initialize exiftool: %w", err)
	}

	return &FlickrExporter{
		client:    client,
		outputDir: outputDir,
		et:        et,
		verbose:   verbose,
	}, nil
}

func (fe *FlickrExporter) Close() {
	if fe.et != nil {
		fe.et.Close()
	}
}

func (fe *FlickrExporter) ExportAlbum(albumID string) error {
	defer fe.Close()

	fmt.Printf("Exporting album %s...\n", albumID)

	album, err := fe.getAlbumInfo(albumID)
	if err != nil {
		return fmt.Errorf("failed to get album info: %w", err)
	}

	photos, err := fe.getAlbumPhotos(albumID)
	if err != nil {
		return fmt.Errorf("failed to get album photos: %w", err)
	}

	album.Photos = photos

	return fe.downloadAlbum(album)
}

func (fe *FlickrExporter) ExportCollection(collectionID string) error {
	defer fe.Close()

	albums, collectionName, err := fe.getCollectionAlbums(collectionID)
	if err != nil {
		return fmt.Errorf("failed to get collection albums: %w", err)
	}

	// Log the collection name if we have it
	if collectionName != "" {
		fmt.Printf("Collection: %s\n", collectionName)
	}

	for _, album := range albums {
		fmt.Printf("Processing album: %s\n", album.Title)
		photos, err := fe.getAlbumPhotos(album.ID)
		if err != nil {
			fmt.Printf("Warning: Failed to get photos for album %s: %v\n", album.ID, err)
			continue
		}
		album.Photos = photos

		if err := fe.downloadAlbum(album); err != nil {
			fmt.Printf("Warning: Failed to download album %s: %v\n", album.ID, err)
		}
	}

	return nil
}

func (fe *FlickrExporter) ExportAllPhotos() error {
	defer fe.Close()

	albums, err := fe.getAllAlbums()
	if err != nil {
		return fmt.Errorf("failed to get all albums: %w", err)
	}

	fmt.Printf("Found %d albums, processing with 4 concurrent workers...\n", len(albums))

	// Track downloaded filenames across all workers
	downloadedFiles := make(map[string]bool)
	var downloadedFilesMutex sync.Mutex

	// Create a work queue for albums
	albumChan := make(chan Album, len(albums))
	errorChan := make(chan error, len(albums))

	// Start 4 worker goroutines, each with their own exporter instance
	var wg sync.WaitGroup
	const numWorkers = 4

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			// Create a separate exporter for this worker to avoid race conditions
			workerET, err := exiftool.NewExiftool()
			if err != nil {
				errorChan <- fmt.Errorf("worker %d: could not initialize exiftool: %w", workerID, err)
				return
			}
			defer workerET.Close()
			
			workerExporter := &FlickrExporter{
				client:    flickr.NewFlickrClient(fe.client.ApiKey, fe.client.ApiSecret),
				outputDir: fe.outputDir,
				et:        workerET,
				verbose:   fe.verbose,
			}
			workerExporter.client.OAuthToken = fe.client.OAuthToken
			workerExporter.client.OAuthTokenSecret = fe.client.OAuthTokenSecret

			fe.albumWorkerWithTracking(workerID, workerExporter, albumChan, errorChan, downloadedFiles, &downloadedFilesMutex)
		}(i)
	}

	// Send albums to workers
	for _, album := range albums {
		albumChan <- album
	}
	close(albumChan)

	// Wait for all workers to complete
	wg.Wait()
	close(errorChan)

	// Collect and report errors
	var errors []error
	for err := range errorChan {
		if err != nil {
			errors = append(errors, err)
		}
	}

	// Download unorganized photos (photos not in any photoset)
	fmt.Println("\nProcessing unorganized photos...")
	unorganizedErr := fe.downloadUnorganizedPhotos(downloadedFiles)
	if unorganizedErr != nil {
		errors = append(errors, unorganizedErr)
	}

	if len(errors) > 0 {
		fmt.Printf("Completed with %d errors\n", len(errors))
		for _, err := range errors {
			fmt.Printf("  Error: %v\n", err)
		}
		return fmt.Errorf("export completed with %d errors", len(errors))
	} else {
		fmt.Println("All photos processed successfully!")
	}

	return nil
}

func (fe *FlickrExporter) albumWorker(workerID int, workerExporter *FlickrExporter, albumChan <-chan Album, errorChan chan<- error) {
	for album := range albumChan {
		fmt.Printf("[Worker %d] Processing album: %s\n", workerID, album.Title)

		// Get photos for this album using the worker's exporter
		photos, err := workerExporter.getAlbumPhotos(album.ID)
		if err != nil {
			errorChan <- fmt.Errorf("worker %d: failed to get photos for album %s: %w", workerID, album.Title, err)
			continue
		}
		album.Photos = photos

		// Download the album using the worker's exporter
		err = workerExporter.downloadAlbum(album)
		if err != nil {
			errorChan <- fmt.Errorf("worker %d: failed to download album %s: %w", workerID, album.Title, err)
			continue
		}

		fmt.Printf("[Worker %d] Completed album: %s (%d photos)\n", workerID, album.Title, len(photos))
		errorChan <- nil // Signal successful completion
	}
}

func (fe *FlickrExporter) albumWorkerWithTracking(workerID int, workerExporter *FlickrExporter, albumChan <-chan Album, errorChan chan<- error, downloadedFiles map[string]bool, mutex *sync.Mutex) {
	for album := range albumChan {
		fmt.Printf("[Worker %d] Processing album: %s\n", workerID, album.Title)

		// Get photos for this album using the worker's exporter
		photos, err := workerExporter.getAlbumPhotos(album.ID)
		if err != nil {
			errorChan <- fmt.Errorf("worker %d: failed to get photos for album %s: %w", workerID, album.Title, err)
			continue
		}
		album.Photos = photos

		// Track filenames before downloading
		mutex.Lock()
		for _, photo := range photos {
			downloadedFiles[photo.Filename] = true
		}
		mutex.Unlock()

		// Download the album using the worker's exporter
		err = workerExporter.downloadAlbum(album)
		if err != nil {
			errorChan <- fmt.Errorf("worker %d: failed to download album %s: %w", workerID, album.Title, err)
			continue
		}

		fmt.Printf("[Worker %d] Completed album: %s (%d photos)\n", workerID, album.Title, len(photos))
		errorChan <- nil // Signal successful completion
	}
}

func (fe *FlickrExporter) getAlbumInfo(albumID string) (Album, error) {
	response, err := photosets.GetInfo(fe.client, false, albumID, "")
	if err != nil {
		return Album{}, err
	}

	// Parse the response to extract album info
	title := response.Set.Title
	description := response.Set.Description
	var dateCreated time.Time

	// Parse date created from timestamp (it's an int in the struct)
	if response.Set.DateCreate > 0 {
		dateCreated = time.Unix(int64(response.Set.DateCreate), 0)
	}

	if dateCreated.IsZero() {
		dateCreated = time.Now()
	}

	return Album{
		ID:          albumID,
		Title:       title,
		Description: description,
		DateCreated: dateCreated,
	}, nil
}

func (fe *FlickrExporter) getAlbumPhotos(albumID string) ([]Photo, error) {
	var photos []Photo
	page := 1
	
	for {
		// Get photos in the album with original URLs
		response, err := photosets.GetPhotos(fe.client, false, albumID, "", page)
		if err != nil {
			return nil, fmt.Errorf("failed to get photos page %d: %w", page, err)
		}

		// Parse the response using the typed structure
		for _, photoData := range response.Photoset.Photos {
			photo, err := fe.parsePhotoFromStruct(photoData)
			if err != nil {
				fmt.Printf("Warning: Failed to get metadata for photo %s: %v\n", photoData.Id, err)
				continue // Skip this photo but continue with others
			}
			if photo.OriginalURL != "" {
				photos = append(photos, photo)
			}
		}

		// Check if we've got all pages
		if page >= response.Photoset.Pages {
			break
		}
		page++
		
		// Rate limiting between API calls
		time.Sleep(100 * time.Millisecond)
	}

	return photos, nil
}

func (fe *FlickrExporter) getCollectionAlbums(collectionID string) ([]Album, string, error) {
	// Use the collections.getTree API to get albums in a collection
	fe.client.Init()
	fe.client.Args.Set("method", "flickr.collections.getTree")
	fe.client.Args.Set("collection_id", collectionID)

	// Sign the request (collections might need OAuth)
	fe.client.OAuthSign()

	response := &CollectionsResponse{}
	err := flickr.DoGet(fe.client, response)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get collection tree: %w", err)
	}

	if response.HasErrors() {
		return nil, "", fmt.Errorf("flickr API error: %s", response.ErrorMsg())
	}

	var albums []Album
	var collectionName string

	// Parse the response - collections can contain sets
	for _, collection := range response.Collections {
		if collectionName == "" {
			collectionName = collection.Title
		}
		for _, set := range collection.Sets {
			album := fe.parseAlbumFromCollectionSet(set)
			albums = append(albums, album)
		}
	}

	if len(albums) == 0 {
		return nil, collectionName, fmt.Errorf("no albums found in collection %s", collectionID)
	}

	return albums, collectionName, nil
}

func (fe *FlickrExporter) getAllAlbums() ([]Album, error) {
	var albums []Album
	page := 1
	
	for {
		response, err := photosets.GetList(fe.client, true, "", page)
		if err != nil {
			return nil, fmt.Errorf("failed to get photosets page %d: %w", page, err)
		}

		// Parse the response using the typed structure
		for _, photosetData := range response.Photosets.Items {
			album := fe.parseAlbumFromStruct(photosetData)
			albums = append(albums, album)
		}

		// Check if we've got all pages
		if page >= response.Photosets.Pages {
			break
		}
		page++
		
		// Rate limiting between API calls
		time.Sleep(100 * time.Millisecond)
	}

	return albums, nil
}

func (fe *FlickrExporter) parsePhotoFromStruct(photoData photosets.Photo) (Photo, error) {
	photo := Photo{
		ID:          photoData.Id,
		Title:       photoData.Title,
		OriginalURL: photoData.URLO,
	}

	// Extract filename from URL
	if photo.OriginalURL != "" {
		parts := strings.Split(photo.OriginalURL, "/")
		if len(parts) > 0 {
			photo.Filename = parts[len(parts)-1]
		}
	}

	// Don't fetch metadata here - we'll do it later only if needed
	return photo, nil
}

func (fe *FlickrExporter) parseAlbumFromStruct(photosetData photosets.Photoset) Album {
	album := Album{
		ID:          photosetData.Id,
		Title:       photosetData.Title,
		Description: photosetData.Description,
	}

	// Parse date created from timestamp (it's an int in the struct)
	if photosetData.DateCreate > 0 {
		album.DateCreated = time.Unix(int64(photosetData.DateCreate), 0)
	}

	// If we can't parse the date, use Unix epoch (1970-01-01) instead of current date
	if album.DateCreated.IsZero() {
		album.DateCreated = time.Unix(0, 0)
	}

	return album
}

func (fe *FlickrExporter) parseAlbumFromCollectionSet(set CollectionSet) Album {
	// Collections API doesn't include full album metadata, so fetch it separately
	albumInfo, err := fe.getAlbumInfo(set.ID)
	if err != nil {
		fmt.Printf("Warning: Failed to get full album info for %s: %v\n", set.Title, err)
		// Fallback to basic info from collection
		return Album{
			ID:          set.ID,
			Title:       set.Title,
			Description: set.Description,
			DateCreated: time.Unix(0, 0), // Use epoch as fallback
		}
	}

	// Use the full album info which has the correct creation date
	return albumInfo
}

func (fe *FlickrExporter) downloadAlbum(album Album) error {
	// Create album directory with date prefix
	datePrefix := album.DateCreated.Format("2006-01-02")
	albumDir := fmt.Sprintf("%s %s", datePrefix, sanitizeFilename(album.Title))
	albumPath := filepath.Join(fe.outputDir, albumDir)

	if err := os.MkdirAll(albumPath, 0755); err != nil {
		return fmt.Errorf("failed to create album directory: %w", err)
	}

	fmt.Printf("Downloading %d photos to %s\n", len(album.Photos), albumPath)

	var failedDownloads []string

	for i, photo := range album.Photos {
		if fe.verbose {
			fmt.Printf("Downloading photo %d/%d: %s\n", i+1, len(album.Photos), photo.Title)
		}

		photoPath := filepath.Join(albumPath, photo.Filename)

		// Check if photo already exists to avoid redownloading
		if _, err := os.Stat(photoPath); err == nil {
			if fe.verbose {
				fmt.Printf("  Skipping (already exists): %s\n", photo.Filename)
			}
			continue
		}

		// Fetch metadata only when we need to download
		if err := fe.fetchPhotoMetadata(&photo); err != nil {
			fmt.Printf("  Warning: Failed to get metadata for %s: %v\n", photo.Filename, err)
			failedDownloads = append(failedDownloads, photo.Filename)
			continue
		}

		if err := fe.downloadPhoto(photo, photoPath); err != nil {
			fmt.Printf("  Warning: Failed to download %s: %v\n", photo.Filename, err)
			failedDownloads = append(failedDownloads, photo.Filename)
			continue
		}

		// Write metadata - this is critical, remove photo if it fails
		if err := fe.writeMetadata(photoPath, photo); err != nil {
			fmt.Printf("  Error: Failed to write metadata for %s: %v\n", photo.Filename, err)
			// Remove the downloaded photo since we can't write metadata
			if removeErr := os.Remove(photoPath); removeErr != nil {
				fmt.Printf("  Error: Also failed to remove incomplete photo %s: %v\n", photo.Filename, removeErr)
			}
			failedDownloads = append(failedDownloads, photo.Filename)
			continue
		}

		// Rate limiting: sleep 100ms between downloads
		if i < len(album.Photos)-1 { // Don't sleep after the last photo
			time.Sleep(100 * time.Millisecond)
		}
	}

	if len(failedDownloads) > 0 {
		return fmt.Errorf("failed to download %d photos: %v", len(failedDownloads), failedDownloads)
	}

	return nil
}

func (fe *FlickrExporter) downloadPhoto(photo Photo, outputPath string) error {
	// First attempt
	err := fe.downloadPhotoAttempt(photo.OriginalURL, outputPath)
	if err == nil {
		return nil
	}

	// Check if it's a 429 (Too Many Requests) error
	if strings.Contains(err.Error(), "HTTP 429") {
		if fe.verbose {
			fmt.Printf("  Rate limited, waiting 5 seconds before retry...\n")
		}
		time.Sleep(5 * time.Second)

		// Retry once
		retryErr := fe.downloadPhotoAttempt(photo.OriginalURL, outputPath)
		if retryErr == nil {
			return nil
		}
		// Return the retry error if both attempts failed
		return fmt.Errorf("failed after retry: %w", retryErr)
	}

	// Return original error if it wasn't a 429
	return err
}

func (fe *FlickrExporter) downloadPhotoAttempt(url, outputPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	return err
}

func (fe *FlickrExporter) writeMetadata(photoPath string, photo Photo) error {
	if fe.et == nil {
		return nil // ExifTool not available
	}

	// Create a FileMetadata object
	fm := exiftool.EmptyFileMetadata()
	fm.File = photoPath

	// Only set fields if they have content from Flickr
	// Set IPTC metadata - only if not empty
	if photo.Title != "" {
		fm.SetString("IPTC:ObjectName", photo.Title) // IPTC - Status / Title
	}
	if photo.Description != "" {
		fm.SetString("IPTC:Caption-Abstract", photo.Description) // IPTC - Content / Description
	}

	// Add keywords - only if we have tags
	if len(photo.Tags) > 0 {
		fm.SetStrings("IPTC:Keywords", photo.Tags)
		fm.SetStrings("XMP:Subject", photo.Tags)
	}

	// Use overwrite_original to preserve existing metadata while adding our fields
	fm.SetString("-overwrite_original", "")

	// Write metadata
	fe.et.WriteMetadata([]exiftool.FileMetadata{fm})

	// Check for errors
	if fm.Err != nil {
		return fm.Err
	}

	return nil
}

func (fe *FlickrExporter) downloadUnorganizedPhotos(downloadedFiles map[string]bool) error {
	fmt.Println("Getting all photos from your Flickr account...")

	// Get all photos from the user's account
	allPhotos, err := fe.getAllPhotos()
	if err != nil {
		return fmt.Errorf("failed to get all photos: %w", err)
	}

	// Filter out photos that were already downloaded in photosets
	var unorganizedPhotos []Photo
	for _, photo := range allPhotos {
		if !downloadedFiles[photo.Filename] {
			unorganizedPhotos = append(unorganizedPhotos, photo)
		}
	}

	if len(unorganizedPhotos) == 0 {
		fmt.Println("No unorganized photos found - all photos are in photosets!")
		return nil
	}

	fmt.Printf("Found %d unorganized photos to download, processing with 4 concurrent workers...\n", len(unorganizedPhotos))

	// Create "Unorganized Photos" directory
	unorganizedDir := filepath.Join(fe.outputDir, "Unorganized Photos")
	if err := os.MkdirAll(unorganizedDir, 0755); err != nil {
		return fmt.Errorf("failed to create unorganized photos directory: %w", err)
	}

	// Create a work queue for photos
	photoChan := make(chan Photo, len(unorganizedPhotos))
	errorChan := make(chan error, len(unorganizedPhotos))

	// Start 4 worker goroutines
	var wg sync.WaitGroup
	const numWorkers = 4

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			// Create a separate exporter for this worker to avoid race conditions
			workerET, err := exiftool.NewExiftool()
			if err != nil {
				errorChan <- fmt.Errorf("worker %d: could not initialize exiftool: %w", workerID, err)
				return
			}
			defer workerET.Close()
			
			workerExporter := &FlickrExporter{
				client:    flickr.NewFlickrClient(fe.client.ApiKey, fe.client.ApiSecret),
				outputDir: fe.outputDir,
				et:        workerET,
				verbose:   fe.verbose,
			}
			workerExporter.client.OAuthToken = fe.client.OAuthToken
			workerExporter.client.OAuthTokenSecret = fe.client.OAuthTokenSecret

			fe.unorganizedPhotoWorker(workerID, workerExporter, photoChan, errorChan, unorganizedDir)
		}(i)
	}

	// Send photos to workers
	for _, photo := range unorganizedPhotos {
		photoChan <- photo
	}
	close(photoChan)

	// Wait for all workers to complete
	wg.Wait()
	close(errorChan)

	// Collect and report errors
	var errors []error
	successCount := 0
	for err := range errorChan {
		if err != nil {
			errors = append(errors, err)
		} else {
			successCount++
		}
	}

	if len(errors) > 0 {
		fmt.Printf("Downloaded %d unorganized photos with %d errors\n", successCount, len(errors))
		for _, err := range errors {
			fmt.Printf("  Error: %v\n", err)
		}
		return fmt.Errorf("failed to download %d unorganized photos", len(errors))
	}

	fmt.Printf("Successfully downloaded %d unorganized photos\n", successCount)
	return nil
}

func (fe *FlickrExporter) unorganizedPhotoWorker(workerID int, workerExporter *FlickrExporter, photoChan <-chan Photo, errorChan chan<- error, unorganizedDir string) {
	for photo := range photoChan {
		if workerExporter.verbose {
			fmt.Printf("[Worker %d] Downloading unorganized photo: %s\n", workerID, photo.Title)
		}

		photoPath := filepath.Join(unorganizedDir, photo.Filename)

		// Check if photo already exists
		if _, err := os.Stat(photoPath); err == nil {
			if workerExporter.verbose {
				fmt.Printf("[Worker %d] Skipping (already exists): %s\n", workerID, photo.Filename)
			}
			errorChan <- nil // Signal successful completion (skip)
			continue
		}

		// Fetch metadata only when we need to download
		if err := workerExporter.fetchPhotoMetadata(&photo); err != nil {
			errorChan <- fmt.Errorf("worker %d: failed to get metadata for %s: %w", workerID, photo.Filename, err)
			continue
		}

		if err := workerExporter.downloadPhoto(photo, photoPath); err != nil {
			errorChan <- fmt.Errorf("worker %d: failed to download %s: %w", workerID, photo.Filename, err)
			continue
		}

		// Write metadata - this is critical, remove photo if it fails
		if err := workerExporter.writeMetadata(photoPath, photo); err != nil {
			fmt.Printf("[Worker %d] Error: Failed to write metadata for %s: %v\n", workerID, photo.Filename, err)
			// Remove the downloaded photo since we can't write metadata
			if removeErr := os.Remove(photoPath); removeErr != nil {
				fmt.Printf("[Worker %d] Error: Also failed to remove incomplete photo %s: %v\n", workerID, photo.Filename, removeErr)
			}
			errorChan <- fmt.Errorf("worker %d: failed to write metadata for %s: %w", workerID, photo.Filename, err)
			continue
		}

		// Rate limiting: sleep 100ms between downloads
		time.Sleep(100 * time.Millisecond)

		errorChan <- nil // Signal successful completion
	}
}

func (fe *FlickrExporter) getAllPhotos() ([]Photo, error) {
	var allPhotos []Photo
	page := 1

	for {
		// Re-initialize the client for each page request
		fe.client.Init()
		fe.client.Args.Set("method", "flickr.people.getPhotos")
		fe.client.Args.Set("user_id", "me")
		fe.client.Args.Set("extras", "original_format,url_o")
		fe.client.Args.Set("per_page", "500")
		fe.client.Args.Set("page", fmt.Sprintf("%d", page))
		fe.client.OAuthSign()

		response := &PhotosResponse{}
		err := flickr.DoGet(fe.client, response)
		if err != nil {
			return nil, fmt.Errorf("failed to get photos page %d: %w", page, err)
		}

		if response.HasErrors() {
			return nil, fmt.Errorf("flickr API error on page %d: %s", page, response.ErrorMsg())
		}

		fmt.Printf("Fetching page %d/%d: Got %d photos\n", page, response.Photos.Pages, len(response.Photos.Photo))

		// Parse photos from this page
		for _, photoData := range response.Photos.Photo {
			photo, err := fe.parsePhotoFromPhotosAPI(photoData)
			if err != nil {
				fmt.Printf("Warning: Failed to get metadata for photo %s: %v\n", photoData.ID, err)
				continue // Skip this photo but continue with others
			}
			if photo.OriginalURL != "" {
				allPhotos = append(allPhotos, photo)
			}
		}

		// Check if we've got all pages
		if page >= response.Photos.Pages {
			break
		}
		page++

		// Rate limiting between API calls
		time.Sleep(100 * time.Millisecond)
	}

	fmt.Printf("Found %d total photos in your account\n", len(allPhotos))
	return allPhotos, nil
}

// PhotosResponse represents the response from flickr.people.getPhotos
type PhotosResponse struct {
	flickr.BasicResponse
	Photos PhotosData `xml:"photos"`
}

type PhotosData struct {
	Page    int         `xml:"page,attr"`
	Pages   int         `xml:"pages,attr"`
	PerPage int         `xml:"perpage,attr"`
	Total   int         `xml:"total,attr"`
	Photo   []PhotoItem `xml:"photo"`
}

type PhotoItem struct {
	ID          string `xml:"id,attr"`
	Title       string `xml:"title,attr"`
	OriginalURL string `xml:"url_o,attr"`
}

func (fe *FlickrExporter) parsePhotoFromPhotosAPI(photoData PhotoItem) (Photo, error) {
	photo := Photo{
		ID:          photoData.ID,
		Title:       photoData.Title,
		OriginalURL: photoData.OriginalURL,
	}

	// Extract filename from URL
	if photo.OriginalURL != "" {
		parts := strings.Split(photo.OriginalURL, "/")
		if len(parts) > 0 {
			photo.Filename = parts[len(parts)-1]
		}
	}

	// Don't fetch metadata here - we'll do it later only if needed
	return photo, nil
}

func (fe *FlickrExporter) fetchPhotoMetadata(photo *Photo) error {
	detailedPhoto, err := fe.getPhotoInfo(photo.ID)
	if err != nil {
		return fmt.Errorf("failed to get metadata for photo %s (%s): %w", photo.ID, photo.Title, err)
	}
	photo.Description = detailedPhoto.Description
	photo.Tags = detailedPhoto.Tags
	photo.DateTaken = detailedPhoto.DateTaken
	return nil
}

func (fe *FlickrExporter) getPhotoInfo(photoID string) (Photo, error) {
	maxRetries := 5
	baseDelay := 2 * time.Second
	
	for attempt := 0; attempt < maxRetries; attempt++ {
		fe.client.Init()
		fe.client.Args.Set("method", "flickr.photos.getInfo")
		fe.client.Args.Set("photo_id", photoID)
		fe.client.OAuthSign()

		response := &PhotoInfoResponse{}
		err := flickr.DoGet(fe.client, response)
		if err != nil {
			// Check if it's a rate limiting error
			if strings.Contains(err.Error(), "HTTP 429") || strings.Contains(err.Error(), "rate limit") {
				if attempt < maxRetries-1 {
					delay := baseDelay * time.Duration(1<<attempt) // Exponential backoff
					if fe.verbose {
						fmt.Printf("Rate limited getting photo info for %s, retrying in %v (attempt %d/%d)\n", photoID, delay, attempt+1, maxRetries)
					}
					time.Sleep(delay)
					continue
				}
			}
			return Photo{}, fmt.Errorf("failed to get photo info for %s after %d attempts: %w", photoID, maxRetries, err)
		}

		if response.HasErrors() {
			// Check if the error message indicates rate limiting
			errorMsg := response.ErrorMsg()
			if strings.Contains(errorMsg, "rate limit") || strings.Contains(errorMsg, "too many requests") {
				if attempt < maxRetries-1 {
					delay := baseDelay * time.Duration(1<<attempt) // Exponential backoff
					if fe.verbose {
						fmt.Printf("Rate limited getting photo info for %s, retrying in %v (attempt %d/%d)\n", photoID, delay, attempt+1, maxRetries)
					}
					time.Sleep(delay)
					continue
				}
			}
			return Photo{}, fmt.Errorf("flickr API error for photo %s: %s", photoID, errorMsg)
		}

		// Success! Parse the response
		var tags []string
		for _, tag := range response.Photo.Tags.Tag {
			tags = append(tags, tag.Raw)
		}

		// Parse date taken
		var dateTaken time.Time
		if response.Photo.Dates.Taken != "" {
			if parsed, err := time.Parse("2006-01-02 15:04:05", response.Photo.Dates.Taken); err == nil {
				dateTaken = parsed
			}
		}

		return Photo{
			ID:          photoID,
			Title:       response.Photo.Title.Content,
			Description: response.Photo.Description.Content,
			Tags:        tags,
			DateTaken:   dateTaken,
		}, nil
	}
	
	return Photo{}, fmt.Errorf("failed to get photo info for %s after %d retry attempts", photoID, maxRetries)
}

// PhotoInfoResponse represents the response from flickr.photos.getInfo
type PhotoInfoResponse struct {
	flickr.BasicResponse
	Photo PhotoInfoDetail `xml:"photo"`
}

type PhotoInfoDetail struct {
	ID          string                `xml:"id,attr"`
	Title       PhotoInfoTitle        `xml:"title"`
	Description PhotoInfoDescription  `xml:"description"`
	Tags        PhotoInfoTags         `xml:"tags"`
	Dates       PhotoInfoDates        `xml:"dates"`
}

type PhotoInfoTitle struct {
	Content string `xml:",chardata"`
}

type PhotoInfoDescription struct {
	Content string `xml:",chardata"`
}

type PhotoInfoTags struct {
	Tag []PhotoInfoTag `xml:"tag"`
}

type PhotoInfoTag struct {
	Raw string `xml:"raw,attr"`
}

type PhotoInfoDates struct {
	Taken string `xml:"taken,attr"`
}

func sanitizeFilename(filename string) string {
	// Remove/replace characters that are problematic in filenames
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		"*", "-",
		"?", "-",
		"\"", "-",
		"<", "-",
		">", "-",
		"|", "-",
	)
	return replacer.Replace(filename)
}
