# flickr-exporter

A command-line tool to download and archive your Flickr photos with metadata preservation.

## Features

- Download all photos from your Flickr account
- Download specific albums (photosets) or collections
- Preserve photo metadata (title, description, tags) as EXIF/IPTC data
- Automatic organization by album with date prefixes
- Resume support - skip already downloaded photos
- Concurrent downloads for faster performance
- OAuth authentication with secure credential storage
- Respects Flickr's rate limits with automatic retry logic
- Downloads photos in their original resolution
- Failed downloads are reported at the end of the process

## Requirements & Building

### Requirements
- Go 1.16 or later
- ExifTool (for metadata writing)
  - macOS: `brew install exiftool`
  - Linux: `sudo apt-get install libimage-exiftool-perl`
  - Windows: Download from https://exiftool.org

### Building
```bash
go build -o flickr-exporter
```

## Usage

### Authentication

Before using flickr-exporter, you need to authenticate with Flickr:

1. **Get Flickr API credentials:**
   - Go to https://www.flickr.com/services/apps/create/
   - Choose "Apply for a Non-Commercial Key"
   - Fill out the application form
   - Save your API Key and Secret

2. **Authenticate flickr-exporter:**
   ```bash
   ./flickr-exporter auth -key YOUR_API_KEY -secret YOUR_API_SECRET
   ```
   - This will open your browser to authorize the application
   - Follow the prompts to save your credentials to a file (e.g., `creds.yml`)
   - You only need to do this once

### Download Options

#### Download All Photos
Downloads all photos from your account, organized by album:
```bash
./flickr-exporter -c creds.yml all -o /path/to/output/directory
```

Photos not in any album will be saved to an "Unorganized Photos" folder.

#### Download a Specific Album
```bash
./flickr-exporter -c creds.yml album ALBUM_ID -o /path/to/output/directory
```

To find album IDs:
1. Go to your album on Flickr
2. The URL will be like `https://www.flickr.com/photos/yourusername/albums/72157694563874100`
3. The album ID is the number at the end (e.g., `72157694563874100`)

#### Download a Collection
```bash
./flickr-exporter -c creds.yml collection COLLECTION_ID -o /path/to/output/directory
```

### Additional Options

- `-c, --creds`: Path to credentials file (recommended)
- `-v, --verbose`: Enable verbose output to see detailed progress
- `-o, --output`: Specify output directory (default: current directory)

### Output Structure

Photos are organized as follows:
```
output-directory/
├── 2023-01-15 Vacation Photos/
│   ├── IMG_001.jpg
│   ├── IMG_002.jpg
│   └── ...
├── 2023-02-20 Birthday Party/
│   └── ...
└── Unorganized Photos/
    └── ...
```

Albums are prefixed with their creation date in YYYY-MM-DD format for chronological sorting.

### Metadata Preservation

The following metadata is written to each downloaded photo:

**IPTC Fields:**
- `ObjectName`: Photo title
- `Caption-Abstract`: Photo description
- `Keywords`: Photo tags

**XMP Fields:**
- `Subject`: Photo tags (duplicate of IPTC Keywords for compatibility)

This metadata can be viewed in most photo management applications and is preserved when copying or backing up files.

### Examples

```bash
# Authenticate (one-time setup)
./flickr-exporter auth -key abc123 -secret xyz789

# Save credentials to a file after authentication
# Follow the prompts to save to creds.yml

# Download all photos to Pictures folder
./flickr-exporter -c creds.yml all -o ~/Pictures/Flickr-Backup

# Download a specific album with verbose output
./flickr-exporter -c creds.yml album 72157694563874100 -o ~/Pictures -v

# Download photos from a collection
./flickr-exporter -c creds.yml collection 12345-67890 -o ~/Pictures/Collections
```

## Author

Claude wrote this code with management by Chris Dzombak ([dzombak.com](https://www.dzombak.com) / [github.com/cdzombak](https://www.github.com/cdzombak)).

## License

GNU GPL v3; see [LICENSE](LICENSE) in this repo.
