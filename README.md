# Flickr Photo Exporter

A Go command-line tool to export original-resolution photos from your Flickr account with full metadata preservation.

## Features

- Export single albums, collections, or all photos from your Flickr account
- Downloads photos at original resolution
- Organizes photos into folders by album with date prefixes (e.g., "2025-06-27 Album Name")
- Preserves EXIF/IPTC metadata including:
  - Photo title (IPTC - Status / Title field)
  - Photo description (IPTC - Content / Description field)
  - Keywords/tags from Flickr
- Avoids duplicate downloads - safely resume interrupted exports
- OAuth authentication support

## Prerequisites

1. **ExifTool**: Required for writing IPTC metadata
   ```bash
   # macOS
   brew install exiftool
   
   # Ubuntu/Debian
   sudo apt-get install libimage-exiftool-perl
   
   # Windows
   # Download from https://exiftool.org/
   ```

2. **Flickr API Key**: Get one from https://www.flickr.com/services/api/misc.api_keys.html

## Installation

```bash
git clone https://github.com/yourusername/flickr-exporter
cd flickr-exporter
go build
```

## Usage

### First: Authenticate with Flickr

Before exporting photos, you need to authenticate with Flickr using OAuth.

#### Option 1: Save credentials to a file (Recommended)
```bash
./flickr-exporter auth -k YOUR_API_KEY -s YOUR_API_SECRET --save-creds ./creds.yml
```

This will:
1. Open a URL for you to visit in your browser
2. Ask you to authorize the application  
3. Save all credentials (API keys + OAuth tokens) to `creds.yml`

#### Option 2: Manual token management
```bash
./flickr-exporter auth -k YOUR_API_KEY -s YOUR_API_SECRET
```

This will show you OAuth tokens that you need to save and use manually with subsequent commands.

### Export Photos

#### Using Credentials File (Recommended)
Once you have a credentials file, exporting is simple:

```bash
# Export one or more albums
./flickr-exporter -c ./creds.yml album ALBUM_ID [ALBUM_ID2] [ALBUM_ID3] ...

# Export one or more collections  
./flickr-exporter -c ./creds.yml collection COLLECTION_ID [COLLECTION_ID2] ...

# Export all photos
./flickr-exporter -c ./creds.yml all

# Specify output directory
./flickr-exporter -c ./creds.yml --output ./my-photos album ALBUM_ID
```

#### Using Manual Credentials
If you prefer to specify credentials manually:

```bash
# Export albums
./flickr-exporter -k YOUR_API_KEY -s YOUR_API_SECRET \
  --oauth-token YOUR_OAUTH_TOKEN --oauth-token-secret YOUR_OAUTH_TOKEN_SECRET \
  album ALBUM_ID [ALBUM_ID2] ...

# Export collections
./flickr-exporter -k YOUR_API_KEY -s YOUR_API_SECRET \
  --oauth-token YOUR_OAUTH_TOKEN --oauth-token-secret YOUR_OAUTH_TOKEN_SECRET \
  collection COLLECTION_ID [COLLECTION_ID2] ...

# Export all photos
./flickr-exporter -k YOUR_API_KEY -s YOUR_API_SECRET \
  --oauth-token YOUR_OAUTH_TOKEN --oauth-token-secret YOUR_OAUTH_TOKEN_SECRET \
  all
```

## Commands

- `auth`: Authenticate with Flickr to get OAuth tokens
- `album [id1] [id2] ...`: Export one or more albums by ID
- `collection [id1] [id2] ...`: Export one or more collections by ID  
- `all`: Export all photos from your account

## Global Flags

- `--api-key, -k`: Your Flickr API key
- `--api-secret, -s`: Your Flickr API secret  
- `--creds-file, -c`: Credentials file (YAML format)
- `--output, -o`: Output directory (default: ./flickr-export)
- `--oauth-token`: OAuth token (for manual credential management)
- `--oauth-token-secret`: OAuth token secret (for manual credential management)

## Auth Command Flags

- `--save-creds`: Save credentials to this YAML file

## Credentials File Format

The credentials file is a YAML file with the following structure:

```yaml
api_key: "your_flickr_api_key"
api_secret: "your_flickr_api_secret"  
oauth_token: "your_oauth_token"
oauth_token_secret: "your_oauth_token_secret"
```

The file is created automatically when you use `auth --save-creds`, but you can also create it manually if needed.

## Finding Album and Collection IDs

To find album or collection IDs:

1. Go to your Flickr album/collection in a web browser
2. Look at the URL: `https://www.flickr.com/photos/username/albums/ALBUM_ID`
3. The ID is the number at the end of the URL

## Output Structure

Photos are organized as follows:

```
flickr-export/
├── 2023-01-15 Vacation Photos/
│   ├── photo1.jpg
│   ├── photo2.jpg
│   └── ...
├── 2023-06-20 Wedding/
│   ├── photo3.jpg
│   └── ...
└── Unorganized Photos/
    ├── photo4.jpg
    └── ...
```

## Metadata Preservation

The tool preserves Flickr metadata in IPTC/EXIF fields:

- **Photo Title**: Written to `IPTC:ObjectName` (IPTC - Status / Title)
- **Photo Description**: Written to `IPTC:Caption-Abstract` (IPTC - Content / Description) 
- **Keywords/Tags**: Written to `IPTC:Keywords` and `XMP:Subject`

## Resuming Interrupted Downloads

The tool automatically skips photos that have already been downloaded, making it safe to resume interrupted exports. Simply run the same command again.

## Troubleshooting

### "Could not initialize exiftool"
Install ExifTool using the instructions in the Prerequisites section.

### "API key is required"
Make sure you're providing a valid Flickr API key with the `--api-key` flag.

### Photos not downloading
- Verify your API key has the necessary permissions
- Check that the album/collection IDs are correct
- Ensure you have permission to access the photos (public vs private)

## License

MIT License - see LICENSE file for details.