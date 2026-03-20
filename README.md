# SyncPay

SyncPay is a modern expense-splitting application (inspired by Splitwise) built with a Go/Gin backend and a React (Vite) frontend.

## 🚀 Features
- **Smart Expense Splitting**: Automatic debt calculation and simplification.
- **Activity Feed**: Real-time group activity logs.
- **SplitBot AI**: Integrated AI assistant powered by Gemini 1.5 Flash to answer questions about group expenses and balances.
- **Invite System**: Join groups via unique invite links and claim dummy member profiles.
- **PostgreSQL**: Robust data persistence.

## 🛠️ Tech Stack
- **Backend**: Go (Gin Gonic), GORM (PostgreSQL)
- **Frontend**: React (Vite), Vanilla CSS (Glassmorphism & Dark Mode)
- **AI**: Google Gemini Pro API

## ⚙️ Setup

### 1. Database
SyncPay requires a PostgreSQL database.
- Create a database named `syncpay`.
- Ensure the connection details are correctly set in `backend/.env`.

### 2. Backend
```bash
cd backend
go mod download
go run main.go
```
The backend includes an automatic migration system that will set up the schema in your `syncpay` database on the first run.

### 3. Frontend
```bash
cd frontend
npm install
npm start
```
The frontend will run on `http://localhost:3000` and proxy requests to the backend on `http://localhost:8000`.

## 📄 License
MIT
