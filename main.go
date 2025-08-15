package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	tele "gopkg.in/telebot.v4"
)

type DriveManager struct {
	service  *drive.Service
	folderID string
}

type UploadSession struct {
	chatID        int64
	isActive      bool
	uploadCount   int
	startTime     time.Time
	authenticated bool
}

var uploadSessions = make(map[int64]*UploadSession)

func main() {
	godotenv.Load()

	// Initialize Google Drive service
	driveManager, err := initGoogleDrive()
	if err != nil {
		log.Fatal("Failed to initialize Google Drive:", err)
	}

	pref := tele.Settings{
		Token: os.Getenv("bot_token"),
		Poller: &tele.LongPoller{
			Timeout: 10 * time.Second,
			AllowedUpdates: []string{
				"message",
				"edited_message",
				"channel_post",
				"edited_channel_post",
				"inline_query",
				"callback_query",
				"shipping_query",
				"pre_checkout_query",
				"poll",
				"poll_answer",
				"chat_member",
				"my_chat_member",
			},
		},
	}
	b, err := tele.NewBot(pref)
	if err != nil {
		log.Fatal(err)
		return
	}

	fmt.Println("Bot has started!")

	// Basic start command
	b.Handle("/start", func(c tele.Context) error {
		return c.Send("Welcome to the Mod Uploader Bot! 🎮\n\nCommands:\n/upload - Start uploading .jar files to Google Drive (requires password)\n/done - Finish uploading session\n/list - List all uploaded mods\n/quantity - Get the number of uploaded mods\n\n🔒 Authentication required for uploading files.")
	})

	// Upload command - starts upload session for .jar files
	b.Handle("/upload", func(c tele.Context) error {
		chatID := c.Chat().ID

		// Start new upload session (not authenticated yet)
		uploadSessions[chatID] = &UploadSession{
			chatID:        chatID,
			isActive:      true,
			uploadCount:   0,
			startTime:     time.Now(),
			authenticated: false,
		}

		return c.Send("� Please enter the upload password to continue:")
	})

	// Done command - ends upload session
	b.Handle("/done", func(c tele.Context) error {
		chatID := c.Chat().ID
		session, exists := uploadSessions[chatID]

		if !exists || !session.isActive {
			return c.Send("No active upload session found. Use /upload to start uploading files.")
		}

		// End the session
		session.isActive = false
		delete(uploadSessions, chatID)

		duration := time.Since(session.startTime)
		return c.Send(fmt.Sprintf("✅ Upload session completed!\n\n📊 Files uploaded: %d\n⏱️ Duration: %v\n\nThank you for using the Mod Uploader Bot!", session.uploadCount, duration.Round(time.Second)))
	})

	// Cancel command - cancels upload session
	b.Handle("/cancel", func(c tele.Context) error {
		chatID := c.Chat().ID
		session, exists := uploadSessions[chatID]

		if !exists || !session.isActive {
			return c.Send("No active upload session found.")
		}

		// Cancel the session
		session.isActive = false
		delete(uploadSessions, chatID)

		return c.Send(fmt.Sprintf("❌ Upload session cancelled.\n\n📊 Files uploaded before cancellation: %d", session.uploadCount))
	})

	// Handle document uploads
	b.Handle(tele.OnDocument, func(c tele.Context) error {
		chatID := c.Chat().ID
		session, exists := uploadSessions[chatID]

		// Check if there's an active upload session
		if !exists || !session.isActive {
			return c.Send("No active upload session found. Use /upload to start uploading files.")
		}

		// Check if authenticated
		if !session.authenticated {
			return c.Send("🔒 Please authenticate first with the password. Use /upload and enter the password.")
		}

		doc := c.Message().Document

		// Check if it's a .jar file
		if !strings.HasSuffix(doc.FileName, ".jar") {
			return c.Send("Please send only .jar files.")
		}

		// Send upload progress message
		progressMsg := fmt.Sprintf("⏳ Uploading %s... (%d files uploaded so far)", doc.FileName, session.uploadCount)
		c.Send(progressMsg)

		// Download the file
		reader, err := b.File(&doc.File)
		if err != nil {
			return c.Send("Failed to get file reader: " + err.Error())
		}

		// Upload to Google Drive
		err = driveManager.uploadFile(doc.FileName, reader)
		if err != nil {
			return c.Send("Failed to upload to Google Drive: " + err.Error())
		}

		// Update session count
		session.uploadCount++

		return c.Send(fmt.Sprintf("✅ Successfully uploaded %s to Google Drive!\n\n📊 Total files uploaded: %d\n\nSend more .jar files or use /done to finish.", doc.FileName, session.uploadCount))
	})

	// Handle text messages (for password authentication)
	b.Handle(tele.OnText, func(c tele.Context) error {
		chatID := c.Chat().ID
		session, exists := uploadSessions[chatID]

		// Check if there's an active upload session waiting for password
		if !exists || !session.isActive || session.authenticated {
			return nil // Ignore text messages if no session or already authenticated
		}

		uploadPassword := os.Getenv("upload_password")
		if uploadPassword == "" {
			uploadPassword = "password" // Default password if not set
		}

		// Check password
		if c.Text() == uploadPassword {
			session.authenticated = true
			return c.Send("✅ UBERIIIIIIIIIIIIIIII\n\n📤 Upload session started!\n\nPlease send your .jar files now. I'll upload each one to Google Drive.\n\nUse /done when you're finished uploading, or /cancel to cancel the session.")
		} else {
			// Wrong password - end session
			session.isActive = false
			delete(uploadSessions, chatID)
			return c.Send("❌ Incorrect password. Upload session cancelled.\n\nUse /upload to try again.")
		}
	})

	// List command - shows all uploaded mods
	b.Handle("/list", func(c tele.Context) error {
		files, err := driveManager.listFiles()
		if err != nil {
			return c.Send("Failed to get file list: " + err.Error())
		}

		if len(files) == 0 {
			return c.Send("No mods uploaded yet.")
		}

		var fileList strings.Builder
		fileList.WriteString("📁 Uploaded Mods:\n\n")
		for i, file := range files {
			fileList.WriteString(fmt.Sprintf("%d. %s\n", i+1, file.Name))
		}

		return c.Send(fileList.String())
	})

	// Quantity command - shows number of uploaded mods
	b.Handle("/quantity", func(c tele.Context) error {
		files, err := driveManager.listFiles()
		if err != nil {
			return c.Send("Failed to get file count: " + err.Error())
		}

		return c.Send(fmt.Sprintf("📊 Total number of uploaded mods: %d", len(files)))
	})

	b.Start()
}

func initGoogleDrive() (*DriveManager, error) {
	ctx := context.Background()

	// Read credentials from environment or file
	credentialsPath := os.Getenv("GOOGLE_CREDENTIALS_PATH")
	if credentialsPath == "" {
		credentialsPath = "credentials.json"
	}

	b, err := os.ReadFile(credentialsPath)
	if err != nil {
		return nil, fmt.Errorf("unable to read client secret file: %v", err)
	}

	config, err := google.ConfigFromJSON(b, drive.DriveFileScope)
	if err != nil {
		return nil, fmt.Errorf("unable to parse client secret file to config: %v", err)
	}

	client := getClient(config)

	srv, err := drive.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve Drive client: %v", err)
	}

	// Create or get the mods folder
	folderID := os.Getenv("folder_id") // Optional: specify exact folder ID
	if folderID != "" {
		fmt.Printf("Using specified folder ID: %s\n", folderID)
	} else {
		folderName := os.Getenv("folder_name")
		if folderName == "" {
			folderName = "MinecraftMods"
		}
		var err error
		folderID, err = createOrGetFolder(srv, folderName)
		if err != nil {
			return nil, fmt.Errorf("unable to create/get folder: %v", err)
		}
	}

	return &DriveManager{
		service:  srv,
		folderID: folderID,
	}, nil
}

func getClient(config *oauth2.Config) *http.Client {
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code: %v", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return tok
}

func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

func createOrGetFolder(srv *drive.Service, folderName string) (string, error) {
	// Check if folder already exists (search in all locations, not just root)
	query := fmt.Sprintf("name='%s' and mimeType='application/vnd.google-apps.folder' and trashed=false", folderName)
	r, err := srv.Files.List().Q(query).PageSize(1000).Do()
	if err != nil {
		return "", err
	}

	// If folders found, use the first one
	if len(r.Files) > 0 {
		fmt.Printf("Found existing folder: %s (ID: %s)\n", r.Files[0].Name, r.Files[0].Id)
		return r.Files[0].Id, nil
	}

	// Create new folder only if none exists
	fmt.Printf("Creating new folder: %s\n", folderName)
	folder := &drive.File{
		Name:     folderName,
		MimeType: "application/vnd.google-apps.folder",
	}

	file, err := srv.Files.Create(folder).Do()
	if err != nil {
		return "", err
	}

	fmt.Printf("Created new folder: %s (ID: %s)\n", file.Name, file.Id)
	return file.Id, nil
}

func (dm *DriveManager) uploadFile(fileName string, reader io.Reader) error {
	file := &drive.File{
		Name:    fileName,
		Parents: []string{dm.folderID},
	}

	_, err := dm.service.Files.Create(file).Media(reader).Do()
	return err
}

func (dm *DriveManager) listFiles() ([]*drive.File, error) {
	r, err := dm.service.Files.List().Q(fmt.Sprintf("'%s' in parents", dm.folderID)).Do()
	if err != nil {
		return nil, err
	}
	return r.Files, nil
}
