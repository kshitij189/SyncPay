package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type User struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	Username  string `gorm:"size:150;uniqueIndex" json:"username"`
	Email     string `gorm:"size:254" json:"email"`
	FirstName string `gorm:"size:150" json:"first_name"`
	LastName  string `gorm:"size:150" json:"last_name"`
	Password  string `gorm:"size:255" json:"-"`
	IsDummy   bool   `gorm:"default:false" json:"-"`
}

type Group struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	Name       string    `gorm:"size:100;not null" json:"name"`
	CreatedByID uint     `json:"-"`
	CreatedBy  User      `gorm:"foreignKey:CreatedByID" json:"created_by"`
	Members    []User    `gorm:"many2many:group_members;" json:"members"`
	CreatedAt  time.Time `gorm:"autoCreateTime" json:"created_at"`
	InviteCode string    `gorm:"size:12;uniqueIndex" json:"invite_code"`
}

func (g *Group) BeforeCreate(tx *gorm.DB) error {
	if g.InviteCode == "" {
		g.InviteCode = strings.ReplaceAll(uuid.New().String(), "-", "")[:12]
	}
	return nil
}

type UserDebt struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	GroupID  uint   `json:"group"`
	Group    Group  `gorm:"foreignKey:GroupID;constraint:OnDelete:CASCADE" json:"-"`
	Username string `gorm:"size:150" json:"username"`
	NetDebt  int    `gorm:"default:0" json:"net_debt"`
}

func (UserDebt) TableName() string { return "user_debts" }

type Debt struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	GroupID  uint   `json:"group"`
	Group    Group  `gorm:"foreignKey:GroupID;constraint:OnDelete:CASCADE" json:"-"`
	FromUser string `gorm:"size:150" json:"from_user"`
	ToUser   string `gorm:"size:150" json:"to_user"`
	Amount   int    `json:"amount"`
}

type OptimisedDebt struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	GroupID  uint   `json:"group"`
	Group    Group  `gorm:"foreignKey:GroupID;constraint:OnDelete:CASCADE" json:"-"`
	FromUser string `gorm:"size:150" json:"from_user"`
	ToUser   string `gorm:"size:150" json:"to_user"`
	Amount   int    `json:"amount"`
}

type Expense struct {
	ID        uint              `gorm:"primaryKey" json:"id"`
	GroupID   uint              `json:"group"`
	Group     Group             `gorm:"foreignKey:GroupID;constraint:OnDelete:CASCADE" json:"-"`
	Title     string            `gorm:"size:100" json:"title"`
	Author    string            `gorm:"size:150" json:"author"`
	Lender    string            `gorm:"size:150" json:"lender"`
	Lenders   []ExpenseLender   `gorm:"foreignKey:ExpenseID" json:"lenders"`
	Borrowers []ExpenseBorrower `gorm:"foreignKey:ExpenseID" json:"borrowers"`
	Comments  []ExpenseComment  `gorm:"foreignKey:ExpenseID" json:"comments"`
	Amount    int               `json:"amount"`
	CreatedAt time.Time         `gorm:"autoCreateTime" json:"created_at"`
}

func (e *Expense) BeforeCreate(tx *gorm.DB) error {
	e.Author = strings.ToLower(e.Author)
	e.Lender = strings.ToLower(e.Lender)
	return nil
}

func (e *Expense) BeforeSave(tx *gorm.DB) error {
	e.Author = strings.ToLower(e.Author)
	e.Lender = strings.ToLower(e.Lender)
	return nil
}

type ExpenseLender struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	ExpenseID uint   `json:"-"`
	Username  string `gorm:"size:150" json:"username"`
	Amount    int    `json:"amount"`
}

type ExpenseBorrower struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	ExpenseID uint   `json:"-"`
	Username  string `gorm:"size:150" json:"username"`
	Amount    int    `json:"amount"`
}

type ExpenseComment struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ExpenseID uint      `json:"expense"`
	Username  string    `gorm:"size:150;column:author" json:"author"`
	Text      string    `gorm:"type:text" json:"text"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (ExpenseComment) TableName() string { return "expense_comments" }

type ActivityLog struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	GroupID     uint      `json:"group"`
	Group       Group     `gorm:"foreignKey:GroupID;constraint:OnDelete:CASCADE" json:"-"`
	User        string    `gorm:"size:150" json:"user"`
	Action      string    `gorm:"size:50" json:"action"`
	Description string    `gorm:"size:300" json:"description"`
	CreatedAt   time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// BlacklistedToken stores revoked refresh tokens
type BlacklistedToken struct {
	ID        uint      `gorm:"primaryKey"`
	Token     string    `gorm:"size:512;uniqueIndex"`
	ExpiresAt time.Time
}

// Unique constraints
type GroupMemberUnique struct{}

func init() {
	// Unique constraints are handled via GORM tags and migration
}
