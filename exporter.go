package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/barasher/go-exiftool"
	"gopkg.in/masci/flickr.v3"
	"gopkg.in/masci/flickr.v3/photosets"
)

type FlickrExporter struct {
	client    *flickr.FlickrClient
	outputDir string
	et        *exiftool.Exiftool
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

func NewFlickrExporter(apiKey, apiSecret, oauthToken, oauthTokenSecret, outputDir string) (*FlickrExporter, error) {
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
		fmt.Printf("Warning: Could not initialize exiftool: %v\n", err)
		fmt.Println("EXIF/IPTC metadata will not be written to photos")
	}
	
	return &FlickrExporter{
		client:    client,
		outputDir: outputDir,
		et:        et,
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
	
	// Also get photos not in any album
	fmt.Println("Processing photos not in albums...")
	unorganizedPhotos, err := fe.getUnorganizedPhotos()
	if err != nil {
		fmt.Printf("Warning: Failed to get unorganized photos: %v\n", err)
	} else if len(unorganizedPhotos) > 0 {
		unorganizedAlbum := Album{
			ID:          "unorganized",
			Title:       "Unorganized Photos",
			Description: "Photos not in any album",
			DateCreated: time.Now(),
			Photos:      unorganizedPhotos,
		}
		if err := fe.downloadAlbum(unorganizedAlbum); err != nil {
			fmt.Printf("Warning: Failed to download unorganized photos: %v\n", err)
		}
	}
	
	return nil
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
	// Get photos in the album with original URLs
	response, err := photosets.GetPhotos(fe.client, false, albumID, "", 1)
	if err != nil {
		return nil, err
	}
	
	var photos []Photo
	
	// Parse the response using the typed structure
	for _, photoData := range response.Photoset.Photos {
		photo := fe.parsePhotoFromStruct(photoData)
		if photo.OriginalURL != "" {
			photos = append(photos, photo)
		}
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
	response, err := photosets.GetList(fe.client, true, "", 1)
	if err != nil {
		return nil, err
	}
	
	var albums []Album
	
	// Parse the response using the typed structure
	for _, photosetData := range response.Photosets.Items {
		album := fe.parseAlbumFromStruct(photosetData)
		albums = append(albums, album)
	}
	
	return albums, nil
}

func (fe *FlickrExporter) getUnorganizedPhotos() ([]Photo, error) {
	// Get photos not in any album - this requires a more complex query
	// For now, return empty slice
	return []Photo{}, nil
}

func (fe *FlickrExporter) parsePhotoFromStruct(photoData photosets.Photo) Photo {
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
	
	// Note: The Photo struct doesn't have Description, Tags, or DateTaken fields in the photosets package
	// These would need to be fetched separately using the photos.getInfo API
	
	return photo
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
	
	for i, photo := range album.Photos {
		fmt.Printf("Downloading photo %d/%d: %s\n", i+1, len(album.Photos), photo.Title)
		
		photoPath := filepath.Join(albumPath, photo.Filename)
		
		// Check if photo already exists to avoid redownloading
		if _, err := os.Stat(photoPath); err == nil {
			fmt.Printf("  Skipping (already exists): %s\n", photo.Filename)
			continue
		}
		
		if err := fe.downloadPhoto(photo, photoPath); err != nil {
			fmt.Printf("  Warning: Failed to download %s: %v\n", photo.Filename, err)
			continue
		}
		
		// Write metadata
		if err := fe.writeMetadata(photoPath, photo); err != nil {
			fmt.Printf("  Warning: Failed to write metadata for %s: %v\n", photo.Filename, err)
		}
	}
	
	return nil
}

func (fe *FlickrExporter) downloadPhoto(photo Photo, outputPath string) error {
	resp, err := http.Get(photo.OriginalURL)
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
	
	// Set IPTC metadata
	fm.SetString("IPTC:ObjectName", photo.Title)        // IPTC - Status / Title
	fm.SetString("IPTC:Caption-Abstract", photo.Description) // IPTC - Content / Description
	
	// Add keywords
	if len(photo.Tags) > 0 {
		fm.SetStrings("IPTC:Keywords", photo.Tags)
		fm.SetStrings("XMP:Subject", photo.Tags)
	}
	
	// Write metadata
	fe.et.WriteMetadata([]exiftool.FileMetadata{fm})
	
	// Check for errors
	if fm.Err != nil {
		return fm.Err
	}
	
	return nil
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