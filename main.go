package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/masci/flickr.v3"
	"gopkg.in/yaml.v3"
)

var (
	apiKey           string
	apiSecret        string
	outputDir        string
	oauthToken       string
	oauthTokenSecret string
	credsFile        string
	credsFileSave    string
)

type Credentials struct {
	APIKey           string `yaml:"api_key"`
	APISecret        string `yaml:"api_secret"`
	OAuthToken       string `yaml:"oauth_token"`
	OAuthTokenSecret string `yaml:"oauth_token_secret"`
}

var rootCmd = &cobra.Command{
	Use:   "flickr-exporter",
	Short: "Export original-resolution photos from Flickr",
	Long: `A tool to export original-resolution photos from your Flickr account.
Supports exporting single albums, collections, or all photos.
Photos are organized by album with date prefixes and include EXIF/IPTC metadata.`,
}

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate with Flickr to get OAuth tokens",
	Long: `Start the OAuth authentication flow to get access tokens.
You'll need to visit a URL and authorize the application.`,
	Run: func(cmd *cobra.Command, args []string) {
		err := loadCredsIfProvided()
		if err != nil {
			fmt.Printf("Error loading credentials: %v\n", err)
			os.Exit(1)
		}
		
		if apiKey == "" || apiSecret == "" {
			fmt.Println("Error: Both API key and API secret are required for authentication")
			fmt.Println("Provide them via flags or credentials file (-c)")
			os.Exit(1)
		}
		
		oauthToken, oauthTokenSecret, err := performOAuthFlow(apiKey, apiSecret)
		if err != nil {
			fmt.Printf("Error during authentication: %v\n", err)
			os.Exit(1)
		}
		
		// Save credentials to file if requested
		if credsFileSave != "" {
			creds := Credentials{
				APIKey:           apiKey,
				APISecret:        apiSecret,
				OAuthToken:       oauthToken,
				OAuthTokenSecret: oauthTokenSecret,
			}
			
			err := saveCredentials(credsFileSave, creds)
			if err != nil {
				fmt.Printf("Error saving credentials: %v\n", err)
				os.Exit(1)
			}
			
			fmt.Printf("Credentials saved to %s\n", credsFileSave)
			fmt.Printf("You can now use: ./flickr-exporter -c %s [command]\n", credsFileSave)
		}
	},
}

var albumCmd = &cobra.Command{
	Use:   "album [album-id] [album-id2] ...",
	Short: "Export one or more albums",
	Long:  "Export photos from one or more Flickr albums by their IDs.",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		err := loadCredsIfProvided()
		if err != nil {
			fmt.Printf("Error loading credentials: %v\n", err)
			os.Exit(1)
		}
		
		if apiKey == "" || apiSecret == "" {
			fmt.Println("Error: Both API key and API secret are required")
			fmt.Println("Provide them via flags or credentials file (-c)")
			os.Exit(1)
		}
		
		exporter, err := NewFlickrExporter(apiKey, apiSecret, oauthToken, oauthTokenSecret, outputDir)
		if err != nil {
			fmt.Printf("Error creating exporter: %v\n", err)
			os.Exit(1)
		}
		
		for _, albumID := range args {
			fmt.Printf("Exporting album %s...\n", albumID)
			err := exporter.ExportAlbum(albumID)
			if err != nil {
				fmt.Printf("Error exporting album %s: %v\n", albumID, err)
				continue
			}
			fmt.Printf("Successfully exported album %s\n", albumID)
		}
	},
}

var collectionCmd = &cobra.Command{
	Use:   "collection [collection-id] [collection-id2] ...",
	Short: "Export one or more collections",
	Long:  "Export photos from one or more Flickr collections by their IDs.",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		err := loadCredsIfProvided()
		if err != nil {
			fmt.Printf("Error loading credentials: %v\n", err)
			os.Exit(1)
		}
		
		if apiKey == "" || apiSecret == "" {
			fmt.Println("Error: Both API key and API secret are required")
			fmt.Println("Provide them via flags or credentials file (-c)")
			os.Exit(1)
		}
		
		exporter, err := NewFlickrExporter(apiKey, apiSecret, oauthToken, oauthTokenSecret, outputDir)
		if err != nil {
			fmt.Printf("Error creating exporter: %v\n", err)
			os.Exit(1)
		}
		
		for _, collectionID := range args {
			fmt.Printf("Exporting collection %s...\n", collectionID)
			err := exporter.ExportCollection(collectionID)
			if err != nil {
				fmt.Printf("Error exporting collection %s: %v\n", collectionID, err)
				continue
			}
			fmt.Printf("Successfully exported collection %s\n", collectionID)
		}
	},
}

var allCmd = &cobra.Command{
	Use:   "all",
	Short: "Export all photos",
	Long:  "Export all photos from your Flickr account, organized by album.",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		err := loadCredsIfProvided()
		if err != nil {
			fmt.Printf("Error loading credentials: %v\n", err)
			os.Exit(1)
		}
		
		if apiKey == "" || apiSecret == "" {
			fmt.Println("Error: Both API key and API secret are required")
			fmt.Println("Provide them via flags or credentials file (-c)")
			os.Exit(1)
		}
		
		exporter, err := NewFlickrExporter(apiKey, apiSecret, oauthToken, oauthTokenSecret, outputDir)
		if err != nil {
			fmt.Printf("Error creating exporter: %v\n", err)
			os.Exit(1)
		}
		
		fmt.Println("Exporting all photos...")
		err = exporter.ExportAllPhotos()
		if err != nil {
			fmt.Printf("Error exporting all photos: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Successfully exported all photos")
	},
}

func performOAuthFlow(apiKey, apiSecret string) (string, string, error) {
	client := flickr.NewFlickrClient(apiKey, apiSecret)
	
	// Step 1: Get request token
	fmt.Println("Getting request token...")
	fmt.Printf("Using API Key: %s\n", apiKey)
	fmt.Printf("Using API Secret: %s\n", apiSecret[:8]+"...")
	
	requestTok, err := flickr.GetRequestToken(client)
	if err != nil {
		return "", "", fmt.Errorf("failed to get request token: %w", err)
	}
	
	// Step 2: Get authorization URL
	authUrl, err := flickr.GetAuthorizeUrl(client, requestTok)
	if err != nil {
		return "", "", fmt.Errorf("failed to get authorization URL: %w", err)
	}
	
	// Step 3: Ask user to authorize
	fmt.Printf("\nPlease visit this URL to authorize the application:\n%s\n\n", authUrl)
	fmt.Print("After authorizing, enter the verification code: ")
	
	var verificationCode string
	_, err = fmt.Scanln(&verificationCode)
	if err != nil {
		return "", "", fmt.Errorf("failed to read verification code: %w", err)
	}
	
	// Step 4: Get access token
	fmt.Println("Getting access token...")
	accessTok, err := flickr.GetAccessToken(client, requestTok, verificationCode)
	if err != nil {
		return "", "", fmt.Errorf("failed to get access token: %w", err)
	}
	
	// Step 5: Display tokens
	fmt.Printf("\nAuthentication successful!\n")
	fmt.Printf("OAuth Token: %s\n", accessTok.OAuthToken)
	fmt.Printf("OAuth Token Secret: %s\n", accessTok.OAuthTokenSecret)
	
	if credsFileSave == "" {
		fmt.Printf("\nSave these tokens and use them with:\n")
		fmt.Printf("--oauth-token %s --oauth-token-secret %s\n", accessTok.OAuthToken, accessTok.OAuthTokenSecret)
	}
	
	return accessTok.OAuthToken, accessTok.OAuthTokenSecret, nil
}

func saveCredentials(filename string, creds Credentials) error {
	data, err := yaml.Marshal(creds)
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}
	
	err = os.WriteFile(filename, data, 0600) // Secure permissions
	if err != nil {
		return fmt.Errorf("failed to write credentials file: %w", err)
	}
	
	return nil
}

func loadCredentials(filename string) (*Credentials, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read credentials file: %w", err)
	}
	
	var creds Credentials
	err = yaml.Unmarshal(data, &creds)
	if err != nil {
		return nil, fmt.Errorf("failed to parse credentials file: %w", err)
	}
	
	return &creds, nil
}

func loadCredsIfProvided() error {
	if credsFile == "" {
		return nil // No credentials file specified
	}
	
	creds, err := loadCredentials(credsFile)
	if err != nil {
		return fmt.Errorf("failed to load credentials: %w", err)
	}
	
	// Only override if not already set via flags
	if apiKey == "" {
		apiKey = creds.APIKey
	}
	if apiSecret == "" {
		apiSecret = creds.APISecret
	}
	if oauthToken == "" {
		oauthToken = creds.OAuthToken
	}
	if oauthTokenSecret == "" {
		oauthTokenSecret = creds.OAuthTokenSecret
	}
	
	return nil
}

func init() {
	// Global flags available to all commands
	rootCmd.PersistentFlags().StringVarP(&apiKey, "api-key", "k", "", "Flickr API Key")
	rootCmd.PersistentFlags().StringVarP(&apiSecret, "api-secret", "s", "", "Flickr API Secret")
	rootCmd.PersistentFlags().StringVarP(&outputDir, "output", "o", "./flickr-export", "Output directory for exported photos")
	rootCmd.PersistentFlags().StringVar(&oauthToken, "oauth-token", "", "OAuth token")
	rootCmd.PersistentFlags().StringVar(&oauthTokenSecret, "oauth-token-secret", "", "OAuth token secret")
	rootCmd.PersistentFlags().StringVarP(&credsFile, "creds-file", "c", "", "Credentials file (YAML)")
	
	// Auth command specific flags
	authCmd.Flags().StringVar(&credsFileSave, "save-creds", "", "Save credentials to this YAML file")
	
	// Add subcommands
	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(albumCmd)
	rootCmd.AddCommand(collectionCmd)
	rootCmd.AddCommand(allCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}