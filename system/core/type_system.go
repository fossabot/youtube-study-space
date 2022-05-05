package core

import (
	"app.modules/core/discordbot"
	"app.modules/core/myfirestore"
	"app.modules/core/mylinebot"
	"app.modules/core/youtubebot"
	"time"
)

type System struct {
	Constants                       *SystemConstants
	ProcessedUserId                 string
	ProcessedUserDisplayName        string
	ProcessedUserIsModeratorOrOwner bool
}

// SystemConstants System生成時に初期化すべきフィールド値
type SystemConstants struct {
	FirestoreController *myfirestore.FirestoreController
	liveChatBot         *youtubebot.YoutubeLiveChatBot
	lineBot             *mylinebot.LineBot
	discordBot          *discordbot.DiscordBot
	
	LiveChatBotChannelId string
	MinWorkTimeMin       int
	MaxWorkTimeMin       int
	DefaultWorkTimeMin   int
	
	MinBreakDurationMin     int
	MaxBreakDurationMin     int
	MinBreakIntervalMin     int
	DefaultBreakDurationMin int
	
	DefaultSleepIntervalMilli       int
	CheckDesiredMaxSeatsIntervalSec int
	
	LastResetDailyTotalStudySec           time.Time
	LastTransferCollectionHistoryBigquery time.Time
	LastLongTimeSittingChecked            time.Time
	
	GcpRegion                      string
	GcsFirestoreExportBucketName   string
	CollectionHistoryRetentionDays int
	
	RecentRangeMin     int
	RecentThresholdMin int
	
	CheckLongTimeSittingIntervalMinutes int
}

type CommandDetails struct {
	CommandType   CommandType
	InOption      InOptions
	InfoOption    InfoOption
	MyOptions     []MyOption
	KickOption    KickOption
	CheckOption   CheckOption
	ReportOption  ReportOption
	ChangeOptions []ChangeOption
	MoreOption    MoreOption
	BreakOption   MinutesAndWorkNameOption
	ResumeOption  ResumeOption
}

type CommandType uint

const (
	NotCommand CommandType = iota
	InvalidCommand
	In     // !in
	SeatIn // !席番号
	Out    // !out
	Info   // !info
	My     // !my
	Change // !change
	Seat   // !seat
	Report // !report
	Kick   // !kick
	Check  // !check
	More   // !more
	Rank   // !rank
	Break  // !break
	Resume // !resume
)

type InfoOption struct {
	ShowDetails bool
}

type MyOptionType uint

const (
	RankVisible MyOptionType = iota
	DefaultStudyMin
	FavoriteColor
)

type ChangeOptionType uint

const (
	WorkName ChangeOptionType = iota
	WorkTime
)

type InOptions struct {
	SeatId   int
	WorkName string
	WorkMin  int
}

type MyOption struct {
	Type        MyOptionType
	IntValue    int
	BoolValue   bool
	StringValue string
}

type KickOption struct {
	SeatId int
}

type CheckOption struct {
	SeatId int
}

type ReportOption struct {
	Message string
}

type ChangeOption struct {
	Type        ChangeOptionType
	StringValue string
	IntValue    int
}

type MoreOption struct {
	DurationMin int
}

type ResumeOption struct {
	WorkName string
}

type MinutesAndWorkNameOption struct {
	WorkName    string
	DurationMin int
}

type UserIdTotalStudySecSet struct {
	UserId        string `json:"user_id"`
	TotalStudySec int    `json:"total_study_sec"`
}
