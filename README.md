# GaycraftBot - Minecraft Mod Uploader

A Telegram bot that uploads .jar files to Google Drive and manages them.

## Features

- Upload .jar files to Google Drive
- List all uploaded mods
- Get quantity of uploaded mods
- Works in group chats

## Commands

- `/start` - Show welcome message and available commands
- `/upload` - Start an upload session for .jar files (requires password)
- `/done` - Finish the current upload session and show summary
- `/cancel` - Cancel the current upload session
- `/list` - List all uploaded mods
- `/quantity` - Get the number of uploaded mods

## Upload Process

1. Use `/upload` command
2. Enter the upload password when prompted
3. Send .jar files one by one
4. Use `/done` when finished or `/cancel` to abort

## Setup

### 1. Install Dependencies

```bash
go mod tidy
```

### 2. Create Telegram Bot

1. Message @BotFather on Telegram
2. Create a new bot with `/newbot`
3. Copy the bot token

### 3. Setup Google Drive API

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project or select existing one
3. Enable Google Drive API
4. Create credentials (OAuth 2.0 Client ID)
5. Download the credentials JSON file and save it as `credentials.json` in the project root

### 4. Configure Environment Variables

1. Copy `.env.example` to `.env`
2. Fill in your bot token and credentials path

```bash
cp .env.example .env
```

Edit `.env`:

```env
token=YOUR_TELEGRAM_BOT_TOKEN
GOOGLE_CREDENTIALS_PATH=credentials.json
```

### 5. First Run Authentication

On the first run, the bot will ask you to authenticate with Google Drive:

1. Run the bot: `go run main.go`
2. Click the authentication URL that appears in the console
3. Grant permissions and copy the authorization code
4. Paste the code back into the console

This will create a `token.json` file for future authentication.

## Usage

1. Start the bot: `go run main.go`
2. Add the bot to your Telegram group or message it directly
3. Use `/upload` command to start an upload session
4. Send multiple .jar files one by one (the bot will upload each one)
5. Use `/done` to finish the session and see the summary
6. Use `/list` to see all uploaded mods
7. Use `/quantity` to see how many mods are uploaded

### Upload Session Workflow

1. Send `/upload` - This starts an upload session
2. Send .jar files - Each file will be uploaded to Google Drive automatically
3. Send `/done` - This ends the session and shows you a summary with:
   - Number of files uploaded
   - Duration of the session
4. Alternatively, use `/cancel` to cancel the session without a summary

## File Structure

```text
├── main.go              # Main bot implementation
├── go.mod              # Go module file
├── go.sum              # Go dependencies
├── .env                # Environment variables (create from .env.example)
├── .env.example        # Example environment file
├── credentials.json    # Google Drive API credentials (download from Google Cloud)
├── token.json          # OAuth token (auto-generated on first run)
└── README.md           # This file
```

## Notes

- Only .jar files are accepted for upload
- Files are uploaded to a "MinecraftMods" folder in your Google Drive
- The bot works in both private messages and group chats
- Make sure to keep your credentials.json and token.json files secure
