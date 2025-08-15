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
	readService   *drive.Service // Service Account для чтения
	uploadService *drive.Service // OAuth2 для загрузки
	folderID      string
}

type UploadSession struct {
	chatID      int64
	isActive    bool
	uploadCount int
	startTime   time.Time
}

var uploadSessions = make(map[int64]*UploadSession)
var isFirstTimeSetup = true

func init() {
	// Проверяем, был ли уже введен пароль ранее
	if _, err := os.Stat("password_entered.flag"); err == nil {
		isFirstTimeSetup = false
	}
}

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

		// Start new upload session
		uploadSessions[chatID] = &UploadSession{
			chatID:      chatID,
			isActive:    true,
			uploadCount: 0,
			startTime:   time.Now(),
		}

		if isFirstTimeSetup {
			return c.Send("🔑 Please enter the upload password to continue:")
		} else {
			return c.Send("✅ Upload session started!\n\nPlease send your .jar files now. I'll upload each one to Google Drive.\n\nUse /done when you're finished uploading, or /cancel to cancel the session.")
		}
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
		if isFirstTimeSetup {
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
		if !exists || !session.isActive || !isFirstTimeSetup {
			return nil // Ignore text messages if no session or password already entered
		}

		uploadPassword := os.Getenv("upload_password")
		if uploadPassword == "" {
			uploadPassword = "password" // Default password if not set
		}

		// Check password
		if c.Text() == uploadPassword {
			// Создаем флаг, что пароль введен
			file, err := os.Create("password_entered.flag")
			if err == nil {
				file.Close()
			}
			isFirstTimeSetup = false

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

	// Инициализация OAuth2 для загрузки (ОБЯЗАТЕЛЬНО)
	oauthCredentialsPath := os.Getenv("OAUTH_CREDENTIALS_PATH")
	if oauthCredentialsPath == "" {
		oauthCredentialsPath = "credentials.json"
	}

	b, err := os.ReadFile(oauthCredentialsPath)
	if err != nil {
		return nil, fmt.Errorf("unable to read oauth credentials: %v", err)
	}

	config, err := google.ConfigFromJSON(b, drive.DriveFileScope)
	if err != nil {
		return nil, fmt.Errorf("unable to parse oauth credentials: %v", err)
	}

	client := getClient(config)
	uploadService, err := drive.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to create upload service: %v", err)
	}

	// Инициализация Service Account для чтения (ОПЦИОНАЛЬНО)
	var readService *drive.Service
	serviceCredentialsPath := os.Getenv("SERVICE_CREDENTIALS_PATH")
	if serviceCredentialsPath == "" {
		serviceCredentialsPath = "service_credentials.json"
	}

	// Проверяем, существует ли файл Service Account
	if _, err := os.Stat(serviceCredentialsPath); err == nil {
		// Читаем Service Account credentials
		serviceCredBytes, err := os.ReadFile(serviceCredentialsPath)
		if err != nil {
			fmt.Printf("Warning: Failed to read Service Account credentials, using OAuth2 for reading: %v\n", err)
			readService = uploadService
		} else {
			// Создаем Service Account конфигурацию
			serviceConfig, err := google.JWTConfigFromJSON(serviceCredBytes, drive.DriveReadonlyScope)
			if err != nil {
				fmt.Printf("Warning: Failed to parse Service Account credentials, using OAuth2 for reading: %v\n", err)
				readService = uploadService
			} else {
				readService, err = drive.NewService(ctx, option.WithHTTPClient(serviceConfig.Client(ctx)))
				if err != nil {
					fmt.Printf("Warning: Failed to create Service Account service, using OAuth2 for reading: %v\n", err)
					readService = uploadService
				} else {
					fmt.Println("Using Service Account for reading files")
				}
			}
		}
	} else {
		fmt.Println("Service Account not configured, using OAuth2 for all operations")
		readService = uploadService // Используем OAuth2 для всего
	}

	// Определяем folder ID
	folderID := os.Getenv("folder_id")
	if folderID != "" {
		fmt.Printf("Using specified folder ID: %s\n", folderID)
	} else {
		folderName := os.Getenv("folder_name")
		if folderName == "" {
			folderName = "MinecraftMods"
		}
		folderID, err = createOrGetFolder(uploadService, folderName)
		if err != nil {
			return nil, fmt.Errorf("unable to create/get folder: %v", err)
		}
	}

	return &DriveManager{
		readService:   readService,
		uploadService: uploadService,
		folderID:      folderID,
	}, nil
}

func createOrGetFolder(srv *drive.Service, folderName string) (string, error) {
	// Check if folder already exists (search in all locations, not just root)
	query := fmt.Sprintf("name='%s' and mimeType='application/vnd.google-apps.folder' and trashed=false", folderName)
	r, err := srv.Files.List().
		Q(query).
		PageSize(1000).
		SupportsAllDrives(true).
		IncludeItemsFromAllDrives(true).
		Corpora("allDrives").
		Do()
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

	file, err := srv.Files.Create(folder).
		SupportsAllDrives(true).
		Do()
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

	_, err := dm.uploadService.Files.Create(file).
		Media(reader).
		SupportsAllDrives(true).
		Do()
	return err
}

func (dm *DriveManager) listFiles() ([]*drive.File, error) {
	r, err := dm.readService.Files.List().
		Q(fmt.Sprintf("'%s' in parents and trashed=false", dm.folderID)).
		SupportsAllDrives(true).
		IncludeItemsFromAllDrives(true).
		Corpora("allDrives").
		Fields("files(id,name,mimeType,owners(emailAddress))").
		Do()
	if err != nil {
		return nil, err
	}
	return r.Files, nil
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
