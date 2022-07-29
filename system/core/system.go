package core

import (
	"app.modules/core/customerror"
	"app.modules/core/discordbot"
	"app.modules/core/guardians"
	"app.modules/core/myfirestore"
	"app.modules/core/mylinebot"
	"app.modules/core/utils"
	"app.modules/core/youtubebot"
	"context"
	"github.com/pkg/errors"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

func NewSystem(ctx context.Context, clientOption option.ClientOption) (System, error) {
	fsController, err := myfirestore.NewFirestoreController(ctx, clientOption)
	if err != nil {
		return System{}, err
	}
	
	// credentials
	credentialsDoc, err := fsController.RetrieveCredentialsConfig(ctx)
	if err != nil {
		return System{}, err
	}
	
	// youtube live chat bot
	liveChatBot, err := youtubebot.NewYoutubeLiveChatBot(credentialsDoc.YoutubeLiveChatId, fsController, ctx)
	if err != nil {
		return System{}, err
	}
	
	// line bot
	lineBot, err := mylinebot.NewLineBot(credentialsDoc.LineBotChannelSecret, credentialsDoc.LineBotChannelToken, credentialsDoc.LineBotDestinationLineId)
	if err != nil {
		return System{}, err
	}
	
	// discord bot
	discordBot, err := discordbot.NewDiscordBot(credentialsDoc.DiscordBotToken, credentialsDoc.DiscordBotTextChannelId)
	if err != nil {
		return System{}, err
	}
	
	// core constant values
	constantsConfig, err := fsController.RetrieveSystemConstantsConfig(ctx)
	if err != nil {
		return System{}, err
	}
	
	return System{
		FirestoreController:             fsController,
		LiveChatBot:                     liveChatBot,
		LineBot:                         lineBot,
		DiscordBot:                      discordBot,
		LiveChatBotChannelId:            credentialsDoc.YoutubeBotChannelId,
		MaxWorkTimeMin:                  constantsConfig.MaxWorkTimeMin,
		MinWorkTimeMin:                  constantsConfig.MinWorkTimeMin,
		DefaultWorkTimeMin:              constantsConfig.DefaultWorkTimeMin,
		DefaultSleepIntervalMilli:       constantsConfig.SleepIntervalMilli,
		CheckDesiredMaxSeatsIntervalSec: constantsConfig.CheckDesiredMaxSeatsIntervalSec,
	}, nil
}

func (s *System) SetProcessedUser(userId string, userDisplayName string, isChatModerator bool, isChatOwner bool) {
	s.ProcessedUserId = userId
	s.ProcessedUserDisplayName = userDisplayName
	s.ProcessedUserIsModeratorOrOwner = isChatModerator || isChatOwner
}

func (s *System) CloseFirestoreClient() {
	err := s.FirestoreController.FirestoreClient.Close()
	if err != nil {
		log.Println("failed close firestore client.")
	} else {
		log.Println("successfully closed firestore client.")
	}
}

func (s *System) AdjustMaxSeats(ctx context.Context) error {
	log.Println("AdjustMaxSeats()")
	constants, err := s.FirestoreController.RetrieveSystemConstantsConfig(ctx)
	if err != nil {
		return err
	}
	if constants.DesiredMaxSeats == constants.MaxSeats {
		return nil
	} else if constants.DesiredMaxSeats > constants.MaxSeats { // 席を増やす
		s.SendLiveChatMessage("ルームを増やします⬆", ctx)
		err := s.FirestoreController.SetMaxSeats(constants.DesiredMaxSeats, ctx)
		if err != nil {
			return err
		}
	} else { // 席を減らす
		// max_seatsを減らしても、空席率が設定値以上か確認
		room, err := s.FirestoreController.RetrieveRoom(ctx)
		if err != nil {
			return err
		}
		if int(float32(constants.DesiredMaxSeats)*(1.0-constants.MinVacancyRate)) < len(room.Seats) {
			message := "減らそうとしすぎ。desiredは却下し、desired max seats <= current max seatsとします。desired: " + strconv.Itoa(constants.DesiredMaxSeats) + ", current max seats: " + strconv.Itoa(constants.MaxSeats) + ", current seats: " + strconv.Itoa(len(room.Seats))
			log.Println(message)
			//_ = s.LineBot.SendMessage(message)
			err := s.FirestoreController.SetDesiredMaxSeats(constants.MaxSeats, ctx)
			if err != nil {
				return err
			}
			return nil
		} else {
			// 消えてしまう席にいるユーザーを移動させる
			s.SendLiveChatMessage("人数が減ったためルームを減らします⬇　必要な場合は席を移動してもらうことがあります。", ctx)
			for _, seat := range room.Seats {
				if seat.SeatId > constants.DesiredMaxSeats {
					s.SetProcessedUser(seat.UserId, seat.UserDisplayName, false, false)
					// 移動先の席を探索
					targetSeatId, err := s.MinAvailableSeatId(ctx)
					if err != nil {
						return err
					}
					// 移動させる
					inCommandDetails := CommandDetails{
						CommandType: SeatIn,
						InOptions: InOptions{
							SeatId:   targetSeatId,
							WorkName: seat.WorkName,
							WorkMin:  int(seat.Until.Sub(utils.JstNow()).Minutes()),
						},
					}
					err = s.In(inCommandDetails, ctx)
					if err != nil {
						return err
					}
				}
			}
			// max_seatsを更新
			err := s.FirestoreController.SetMaxSeats(constants.DesiredMaxSeats, ctx)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// Command 入力コマンドを解析して実行
func (s *System) Command(commandString string, userId string, userDisplayName string, isChatModerator bool, isChatOwner bool, ctx context.Context) customerror.CustomError {
	if userId == s.LiveChatBotChannelId {
		return customerror.NewNil()
	}
	s.SetProcessedUser(userId, userDisplayName, isChatModerator, isChatOwner)
	
	commandDetails, err := s.ParseCommand(commandString)
	if err.IsNotNil() { // これはシステム内部のエラーではなく、コマンドが悪いということなので、return nil
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、"+err.Body.Error(), ctx)
		return customerror.NewNil()
	}
	//log.Printf("parsed command: %# v\n", pretty.Formatter(commandDetails))
	
	// commandDetailsに基づいて命令処理
	switch commandDetails.CommandType {
	case NotCommand:
		return customerror.NewNil()
	case InvalidCommand:
		// 暫定で何も反応しない
		return customerror.NewNil()
	case In:
		fallthrough
	case SeatIn:
		err := s.In(commandDetails, ctx)
		if err != nil {
			return customerror.InProcessFailed.New(err.Error())
		}
		return customerror.NewNil()
	case Out:
		err := s.Out(commandDetails, ctx)
		if err != nil {
			return customerror.OutProcessFailed.New(err.Error())
		}
		return customerror.NewNil()
	case Info:
		err := s.ShowUserInfo(commandDetails, ctx)
		if err != nil {
			return customerror.InfoProcessFailed.New(err.Error())
		}
		return customerror.NewNil()
	case My:
		err := s.My(commandDetails, ctx)
		if err != nil {
			return customerror.MyProcessFailed.New(err.Error())
		}
		return customerror.NewNil()
	case Change:
		err := s.Change(commandDetails, ctx)
		if err != nil {
			return customerror.ChangeProcessFailed.New(err.Error())
		}
	case Seat:
		err := s.ShowSeatInfo(commandDetails, ctx)
		if err != nil {
			return customerror.SeatProcessFailed.New(err.Error())
		}
		return customerror.NewNil()
	case Report:
		err := s.Report(commandDetails, ctx)
		if err != nil {
			return customerror.ReportProcessFailed.New(err.Error())
		}
		return customerror.NewNil()
	case Kick:
		err := s.Kick(commandDetails, ctx)
		if err != nil {
			return customerror.KickProcessFailed.New(err.Error())
		}
		return customerror.NewNil()
	case Check:
		err := s.Check(commandDetails, ctx)
		if err != nil {
			return customerror.CheckProcessFailed.New(err.Error())
		}
		return customerror.NewNil()
	case More:
		err := s.More(commandDetails, ctx)
		if err != nil {
			return customerror.AddProcessFailed.New(err.Error())
		}
		return customerror.NewNil()
	case Rank:
		err := s.Rank(commandDetails, ctx)
		if err != nil {
			return customerror.RankProcessFailed.New(err.Error())
		}
		return customerror.NewNil()
	default:
		_ = s.LineBot.SendMessage("Unknown command: " + commandString)
	}
	return customerror.NewNil()
}

// ParseCommand コマンドを解析
func (s *System) ParseCommand(commandString string) (CommandDetails, customerror.CustomError) {
	// 全角スペースを半角に変換
	commandString = strings.Replace(commandString, FullWidthSpace, HalfWidthSpace, -1)
	// 全角イコールを半角に変換
	commandString = strings.Replace(commandString, "＝", "=", -1)
	
	if strings.HasPrefix(commandString, CommandPrefix) {
		slice := strings.Split(commandString, HalfWidthSpace)
		switch slice[0] {
		case InCommand:
			commandDetails, err := s.ParseIn(commandString)
			if err.IsNotNil() {
				return CommandDetails{}, err
			}
			return commandDetails, customerror.NewNil()
		case OutCommand:
			return CommandDetails{
				CommandType: Out,
				InOptions:   InOptions{},
			}, customerror.NewNil()
		case InfoCommand:
			commandDetails, err := s.ParseInfo(commandString)
			if err.IsNotNil() {
				return CommandDetails{}, err
			}
			return commandDetails, customerror.NewNil()
		case MyCommand:
			commandDetails, err := s.ParseMy(commandString)
			if err.IsNotNil() {
				return CommandDetails{}, err
			}
			return commandDetails, customerror.NewNil()
		case ChangeCommand:
			commandDetails, err := s.ParseChange(commandString)
			if err.IsNotNil() {
				return CommandDetails{}, err
			}
			return commandDetails, customerror.NewNil()
		case SeatCommand:
			return CommandDetails{
				CommandType: Seat,
			}, customerror.NewNil()
		case ReportCommand:
			commandDetails, err := s.ParseReport(commandString)
			if err.IsNotNil() {
				return CommandDetails{}, err
			}
			return commandDetails, customerror.NewNil()
		case KickCommand:
			commandDetails, err := s.ParseKick(commandString)
			if err.IsNotNil() {
				return CommandDetails{}, err
			}
			return commandDetails, customerror.NewNil()
		case CheckCommand:
			commandDetails, err := s.ParseCheck(commandString)
			if err.IsNotNil() {
				return CommandDetails{}, err
			}
			return commandDetails, customerror.NewNil()
		case LegacyAddCommand:
			return CommandDetails{}, customerror.InvalidCommand.New("「" + LegacyAddCommand + "」は使えなくなりました。代わりに「" + MoreCommand + "」か「" + OkawariCommand + "」を使ってください")
		case OkawariCommand:
			fallthrough
		case MoreCommand:
			commandDetails, err := s.ParseMore(commandString)
			if err.IsNotNil() {
				return CommandDetails{}, err
			}
			return commandDetails, customerror.NewNil()
		case RankCommand:
			return CommandDetails{
				CommandType: Rank,
			}, customerror.NewNil()
		case CommandPrefix: // 典型的なミスコマンド「! in」「! out」とか。
			return CommandDetails{}, customerror.InvalidCommand.New("びっくりマークは隣の文字とくっつけてください")
		default: // !席番号 or 間違いコマンド
			// !席番号かどうか
			num, err := strconv.Atoi(strings.TrimPrefix(slice[0], CommandPrefix))
			if err == nil && num >= 0 {
				commandDetails, err := s.ParseSeatIn(num, commandString)
				if err.IsNotNil() {
					return CommandDetails{}, err
				}
				return commandDetails, customerror.NewNil()
			}
			
			// 間違いコマンド
			return CommandDetails{
				CommandType: InvalidCommand,
				InOptions:   InOptions{},
			}, customerror.NewNil()
		}
	} else if strings.HasPrefix(commandString, WrongCommandPrefix) {
		return CommandDetails{}, customerror.InvalidCommand.New("びっくりマークは半角にしてください")
	}
	return CommandDetails{
		CommandType: NotCommand,
		InOptions:   InOptions{},
	}, customerror.NewNil()
}

func (s *System) ParseIn(commandString string) (CommandDetails, customerror.CustomError) {
	slice := strings.Split(commandString, HalfWidthSpace)
	
	// 追加オプションチェック
	options, err := s.ParseInOptions(slice[1:])
	if err.IsNotNil() {
		return CommandDetails{}, err
	}
	
	return CommandDetails{
		CommandType: In,
		InOptions:   options,
	}, customerror.NewNil()
}

func (s *System) ParseSeatIn(seatNum int, commandString string) (CommandDetails, customerror.CustomError) {
	slice := strings.Split(commandString, HalfWidthSpace)
	
	// 追加オプションチェック
	options, err := s.ParseInOptions(slice[1:])
	if err.IsNotNil() {
		return CommandDetails{}, err
	}
	
	// 追加オプションに席番号を追加
	options.SeatId = seatNum
	
	return CommandDetails{
		CommandType: SeatIn,
		InOptions:   options,
	}, customerror.NewNil()
}

func (s *System) ParseInOptions(commandSlice []string) (InOptions, customerror.CustomError) {
	workName := ""
	isWorkNameSet := false
	workTimeMin := s.DefaultWorkTimeMin
	isWorkTimeMinSet := false
	for _, str := range commandSlice {
		if strings.HasPrefix(str, WorkNameOptionPrefix) && !isWorkNameSet {
			workName = strings.TrimPrefix(str, WorkNameOptionPrefix)
			isWorkNameSet = true
		} else if strings.HasPrefix(str, WorkNameOptionShortPrefix) && !isWorkNameSet {
			workName = strings.TrimPrefix(str, WorkNameOptionShortPrefix)
			isWorkNameSet = true
		} else if strings.HasPrefix(str, WorkNameOptionPrefixLegacy) && !isWorkNameSet {
			workName = strings.TrimPrefix(str, WorkNameOptionPrefixLegacy)
			isWorkNameSet = true
		} else if strings.HasPrefix(str, WorkNameOptionShortPrefixLegacy) && !isWorkNameSet {
			workName = strings.TrimPrefix(str, WorkNameOptionShortPrefixLegacy)
			isWorkNameSet = true
		} else if strings.HasPrefix(str, WorkTimeOptionPrefix) && !isWorkTimeMinSet {
			num, err := strconv.Atoi(strings.TrimPrefix(str, WorkTimeOptionPrefix))
			if err != nil { // 無効な値
				return InOptions{}, customerror.InvalidCommand.New("「" + WorkTimeOptionPrefix + "」の後の値を確認してください")
			}
			if s.MinWorkTimeMin <= num && num <= s.MaxWorkTimeMin {
				workTimeMin = num
				isWorkTimeMinSet = true
			} else { // 無効な値
				return InOptions{}, customerror.InvalidCommand.New("入室時間（分）は" + strconv.Itoa(s.MinWorkTimeMin) + "～" + strconv.Itoa(s.MaxWorkTimeMin) + "の値にしてください")
			}
		} else if strings.HasPrefix(str, WorkTimeOptionShortPrefix) && !isWorkTimeMinSet {
			num, err := strconv.Atoi(strings.TrimPrefix(str, WorkTimeOptionShortPrefix))
			if err != nil { // 無効な値
				return InOptions{}, customerror.InvalidCommand.New("「" + WorkTimeOptionShortPrefix + "」の後の値を確認してください")
			}
			if s.MinWorkTimeMin <= num && num <= s.MaxWorkTimeMin {
				workTimeMin = num
				isWorkTimeMinSet = true
			} else { // 無効な値
				return InOptions{}, customerror.InvalidCommand.New("入室時間（分）は" + strconv.Itoa(s.MinWorkTimeMin) + "～" + strconv.Itoa(s.MaxWorkTimeMin) + "の値にしてください")
			}
		} else if strings.HasPrefix(str, WorkTimeOptionPrefixLegacy) && !isWorkTimeMinSet {
			num, err := strconv.Atoi(strings.TrimPrefix(str, WorkTimeOptionPrefixLegacy))
			if err != nil { // 無効な値
				return InOptions{}, customerror.InvalidCommand.New("「" + WorkTimeOptionPrefixLegacy + "」の後の値を確認してください")
			}
			if s.MinWorkTimeMin <= num && num <= s.MaxWorkTimeMin {
				workTimeMin = num
				isWorkTimeMinSet = true
			} else { // 無効な値
				return InOptions{}, customerror.InvalidCommand.New("入室時間（分）は" + strconv.Itoa(s.MinWorkTimeMin) + "～" + strconv.Itoa(s.MaxWorkTimeMin) + "の値にしてください")
			}
		} else if strings.HasPrefix(str, WorkTimeOptionShortPrefixLegacy) && !isWorkTimeMinSet {
			num, err := strconv.Atoi(strings.TrimPrefix(str, WorkTimeOptionShortPrefixLegacy))
			if err != nil { // 無効な値
				return InOptions{}, customerror.InvalidCommand.New("「" + WorkTimeOptionShortPrefixLegacy + "」の後の値を確認してください")
			}
			if s.MinWorkTimeMin <= num && num <= s.MaxWorkTimeMin {
				workTimeMin = num
				isWorkTimeMinSet = true
			} else { // 無効な値
				return InOptions{}, customerror.InvalidCommand.New("入室時間（分）は" + strconv.Itoa(s.MinWorkTimeMin) + "～" + strconv.Itoa(s.MaxWorkTimeMin) + "の値にしてください")
			}
		}
	}
	return InOptions{
		SeatId:   -1,
		WorkName: workName,
		WorkMin:  workTimeMin,
	}, customerror.NewNil()
}

func (s *System) ParseInfo(commandString string) (CommandDetails, customerror.CustomError) {
	slice := strings.Split(commandString, HalfWidthSpace)
	
	if len(slice) >= 2 {
		if slice[1] == InfoDetailsOption {
			return CommandDetails{
				CommandType: Info,
				InfoOption: InfoOption{
					ShowDetails: true,
				},
			}, customerror.NewNil()
		}
	}
	return CommandDetails{
		CommandType: Info,
	}, customerror.NewNil()
}

func (s *System) ParseMy(commandString string) (CommandDetails, customerror.CustomError) {
	slice := strings.Split(commandString, HalfWidthSpace)
	
	options, err := s.ParseMyOptions(slice[1:])
	if err.IsNotNil() {
		return CommandDetails{}, err
	}
	
	return CommandDetails{
		CommandType: My,
		MyOptions:   options,
	}, customerror.NewNil()
}

func (s *System) ParseMyOptions(commandSlice []string) ([]MyOption, customerror.CustomError) {
	isRankVisibleSet := false
	
	var options []MyOption
	
	for _, str := range commandSlice {
		if strings.HasPrefix(str, RankVisibleMyOptionPrefix) && !isRankVisibleSet {
			var rankVisible bool
			rankVisibleStr := strings.TrimPrefix(str, RankVisibleMyOptionPrefix)
			if rankVisibleStr == RankVisibleMyOptionOn {
				rankVisible = true
			} else if rankVisibleStr == RankVisibleMyOptionOff {
				rankVisible = false
			} else {
				return []MyOption{}, customerror.InvalidCommand.New("「" + RankVisibleMyOptionPrefix + "」の後の値を確認してください")
			}
			options = append(options, MyOption{
				Type:      RankVisible,
				BoolValue: rankVisible,
			})
			isRankVisibleSet = true
		}
	}
	return options, customerror.NewNil()
}

func (s *System) ParseKick(commandString string) (CommandDetails, customerror.CustomError) {
	slice := strings.Split(commandString, HalfWidthSpace)
	
	var kickSeatId int
	if len(slice) >= 2 {
		num, err := strconv.Atoi(slice[1])
		if err != nil {
			return CommandDetails{}, customerror.InvalidCommand.New("有効な席番号を指定してください")
		}
		kickSeatId = num
	} else {
		return CommandDetails{}, customerror.InvalidCommand.New("席番号を指定してください")
	}
	
	return CommandDetails{
		CommandType: Kick,
		KickSeatId:  kickSeatId,
	}, customerror.NewNil()
}

func (s *System) ParseCheck(commandString string) (CommandDetails, customerror.CustomError) {
	slice := strings.Split(commandString, HalfWidthSpace)
	
	var targetSeatId int
	if len(slice) >= 2 {
		num, err := strconv.Atoi(slice[1])
		if err != nil {
			return CommandDetails{}, customerror.InvalidCommand.New("有効な席番号を指定してください")
		}
		targetSeatId = num
	} else {
		return CommandDetails{}, customerror.InvalidCommand.New("席番号を指定してください")
	}
	
	return CommandDetails{
		CommandType: Check,
		CheckSeatId: targetSeatId,
	}, customerror.NewNil()
}

func (s *System) ParseReport(commandString string) (CommandDetails, customerror.CustomError) {
	slice := strings.Split(commandString, HalfWidthSpace)
	
	var reportMessage string
	if len(slice) == 1 {
		return CommandDetails{}, customerror.InvalidCommand.New("!reportの右にスペースを空けてメッセージを書いてください。")
	} else { // len(slice) > 1
		reportMessage = commandString
	}
	
	return CommandDetails{
		CommandType:   Report,
		ReportMessage: reportMessage,
	}, customerror.NewNil()
}

func (s *System) ParseChange(commandString string) (CommandDetails, customerror.CustomError) {
	slice := strings.Split(commandString, HalfWidthSpace)
	
	// 追加オプションチェック
	options, err := s.ParseChangeOptions(slice[1:])
	if err.IsNotNil() {
		return CommandDetails{}, err
	}
	
	return CommandDetails{
		CommandType:   Change,
		ChangeOptions: options,
	}, customerror.NewNil()
}

func (s *System) ParseChangeOptions(commandSlice []string) ([]ChangeOption, customerror.CustomError) {
	isWorkNameSet := false
	isWorkTimeMinSet := false
	
	var options []ChangeOption
	
	for _, str := range commandSlice {
		if strings.HasPrefix(str, WorkNameOptionPrefix) && !isWorkNameSet {
			workName := strings.TrimPrefix(str, WorkNameOptionPrefix)
			options = append(options, ChangeOption{
				Type:        WorkName,
				StringValue: workName,
			})
			isWorkNameSet = true
		} else if strings.HasPrefix(str, WorkNameOptionShortPrefix) && !isWorkNameSet {
			workName := strings.TrimPrefix(str, WorkNameOptionShortPrefix)
			options = append(options, ChangeOption{
				Type:        WorkName,
				StringValue: workName,
			})
			isWorkNameSet = true
		} else if strings.HasPrefix(str, WorkNameOptionPrefixLegacy) && !isWorkNameSet {
			workName := strings.TrimPrefix(str, WorkNameOptionPrefixLegacy)
			options = append(options, ChangeOption{
				Type:        WorkName,
				StringValue: workName,
			})
			isWorkNameSet = true
		} else if strings.HasPrefix(str, WorkNameOptionShortPrefixLegacy) && !isWorkNameSet {
			workName := strings.TrimPrefix(str, WorkNameOptionShortPrefixLegacy)
			options = append(options, ChangeOption{
				Type:        WorkName,
				StringValue: workName,
			})
			isWorkNameSet = true
		} else if strings.HasPrefix(str, WorkTimeOptionPrefix) && !isWorkTimeMinSet {
			num, err := strconv.Atoi(strings.TrimPrefix(str, WorkTimeOptionPrefix))
			if err != nil { // 無効な値
				return []ChangeOption{}, customerror.InvalidCommand.New("「" + WorkTimeOptionPrefix + "」の後の値を確認してください")
			}
			if s.MinWorkTimeMin <= num { // 延長できるシステムなので、上限はなし
				options = append(options, ChangeOption{
					Type:     WorkTime,
					IntValue: num,
				})
				isWorkTimeMinSet = true
			} else { // 無効な値
				return []ChangeOption{}, customerror.InvalidCommand.New("入室時間（分）は" + strconv.Itoa(s.MinWorkTimeMin) + "以上の値にしてください")
			}
		} else if strings.HasPrefix(str, WorkTimeOptionShortPrefix) && !isWorkTimeMinSet {
			num, err := strconv.Atoi(strings.TrimPrefix(str, WorkTimeOptionShortPrefix))
			if err != nil { // 無効な値
				return []ChangeOption{}, customerror.InvalidCommand.New("「" + WorkTimeOptionShortPrefix + "」の後の値を確認してください")
			}
			if s.MinWorkTimeMin <= num { // 延長できるシステムなので、上限はなし
				options = append(options, ChangeOption{
					Type:     WorkTime,
					IntValue: num,
				})
				isWorkTimeMinSet = true
			} else { // 無効な値
				return []ChangeOption{}, customerror.InvalidCommand.New("入室時間（分）は" + strconv.Itoa(s.MinWorkTimeMin) + "以上の値にしてください")
			}
		} else if strings.HasPrefix(str, WorkTimeOptionPrefixLegacy) && !isWorkTimeMinSet {
			num, err := strconv.Atoi(strings.TrimPrefix(str, WorkTimeOptionPrefixLegacy))
			if err != nil { // 無効な値
				return []ChangeOption{}, customerror.InvalidCommand.New("「" + WorkTimeOptionPrefixLegacy + "」の後の値を確認してください")
			}
			if s.MinWorkTimeMin <= num { // 延長できるシステムなので、上限はなし
				options = append(options, ChangeOption{
					Type:     WorkTime,
					IntValue: num,
				})
				isWorkTimeMinSet = true
			} else { // 無効な値
				return []ChangeOption{}, customerror.InvalidCommand.New("入室時間（分）は" + strconv.Itoa(s.MinWorkTimeMin) + "以上の値にしてください")
			}
		} else if strings.HasPrefix(str, WorkTimeOptionShortPrefixLegacy) && !isWorkTimeMinSet {
			num, err := strconv.Atoi(strings.TrimPrefix(str, WorkTimeOptionShortPrefixLegacy))
			if err != nil { // 無効な値
				return []ChangeOption{}, customerror.InvalidCommand.New("「" + WorkTimeOptionShortPrefixLegacy + "」の後の値を確認してください")
			}
			if s.MinWorkTimeMin <= num { // 延長できるシステムなので、上限はなし
				options = append(options, ChangeOption{
					Type:     WorkTime,
					IntValue: num,
				})
				isWorkTimeMinSet = true
			} else { // 無効な値
				return []ChangeOption{}, customerror.InvalidCommand.New("入室時間（分）は" + strconv.Itoa(s.MinWorkTimeMin) + "以上の値にしてください")
			}
		}
	}
	return options, customerror.NewNil()
}

func (s *System) ParseMore(commandString string) (CommandDetails, customerror.CustomError) {
	slice := strings.Split(commandString, HalfWidthSpace)
	
	// 指定時間
	var workTimeMin int
	if len(slice) >= 2 {
		if strings.HasPrefix(slice[1], WorkTimeOptionPrefix) {
			num, err := strconv.Atoi(strings.TrimPrefix(slice[1], WorkTimeOptionPrefix))
			if err != nil { // 無効な値
				return CommandDetails{}, customerror.InvalidCommand.New("「" + WorkTimeOptionPrefix + "」の後の値を確認してください")
			}
			if s.MinWorkTimeMin <= num && num <= s.MaxWorkTimeMin {
				workTimeMin = num
			} else { // 無効な値
				return CommandDetails{}, customerror.InvalidCommand.New("延長時間（分）は" + strconv.Itoa(s.MinWorkTimeMin) + "～" + strconv.Itoa(s.MaxWorkTimeMin) + "の値にしてください")
			}
		} else if strings.HasPrefix(slice[1], WorkTimeOptionShortPrefix) {
			num, err := strconv.Atoi(strings.TrimPrefix(slice[1], WorkTimeOptionShortPrefix))
			if err != nil { // 無効な値
				return CommandDetails{}, customerror.InvalidCommand.New("「" + WorkTimeOptionShortPrefix + "」の後の値を確認してください")
			}
			if s.MinWorkTimeMin <= num && num <= s.MaxWorkTimeMin {
				workTimeMin = num
			} else { // 無効な値
				return CommandDetails{}, customerror.InvalidCommand.New("延長時間（分）は" + strconv.Itoa(s.MinWorkTimeMin) + "～" + strconv.Itoa(s.MaxWorkTimeMin) + "の値にしてください")
			}
		} else if strings.HasPrefix(slice[1], WorkTimeOptionPrefixLegacy) {
			num, err := strconv.Atoi(strings.TrimPrefix(slice[1], WorkTimeOptionPrefixLegacy))
			if err != nil { // 無効な値
				return CommandDetails{}, customerror.InvalidCommand.New("「" + WorkTimeOptionPrefixLegacy + "」の後の値を確認してください")
			}
			if s.MinWorkTimeMin <= num && num <= s.MaxWorkTimeMin {
				workTimeMin = num
			} else { // 無効な値
				return CommandDetails{}, customerror.InvalidCommand.New("延長時間（分）は" + strconv.Itoa(s.MinWorkTimeMin) + "～" + strconv.Itoa(s.MaxWorkTimeMin) + "の値にしてください")
			}
		} else if strings.HasPrefix(slice[1], WorkTimeOptionShortPrefixLegacy) {
			num, err := strconv.Atoi(strings.TrimPrefix(slice[1], WorkTimeOptionShortPrefixLegacy))
			if err != nil { // 無効な値
				return CommandDetails{}, customerror.InvalidCommand.New("「" + WorkTimeOptionShortPrefixLegacy + "」の後の値を確認してください")
			}
			if s.MinWorkTimeMin <= num && num <= s.MaxWorkTimeMin {
				workTimeMin = num
			} else { // 無効な値
				return CommandDetails{}, customerror.InvalidCommand.New("延長時間（分）は" + strconv.Itoa(s.MinWorkTimeMin) + "～" + strconv.Itoa(s.MaxWorkTimeMin) + "の値にしてください")
			}
		}
	} else {
		return CommandDetails{}, customerror.InvalidCommand.New("延長時間（分）を「" + WorkTimeOptionPrefix + "」で指定してください")
	}
	
	if workTimeMin == 0 {
		return CommandDetails{}, customerror.InvalidCommand.New("オプションが正しく設定されているか確認してください")
	}
	
	return CommandDetails{
		CommandType: More,
		MoreMinutes: workTimeMin,
	}, customerror.NewNil()
}

func (s *System) In(command CommandDetails, ctx context.Context) error {
	// 初回の利用の場合はユーザーデータを初期化
	isRegistered, err := s.IfUserRegistered(ctx)
	if err != nil {
		return err
	}
	if !isRegistered {
		err := s.InitializeUser(ctx)
		if err != nil {
			return err
		}
	}
	
	// 席を指定している場合
	if command.CommandType == SeatIn {
		// 指定された座席番号が有効かチェック
		// その席番号が存在するか
		isSeatExist, err := s.IsSeatExist(command.InOptions.SeatId, ctx)
		if err != nil {
			s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、エラーが発生しました。もう一度試してみてください", ctx)
			_ = s.LineBot.SendMessageWithError("failed s.IsSeatExist()", err)
			return err
		} else if !isSeatExist {
			s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、その番号の席は"+"存在しません。他の空いている席を選ぶか、「"+InCommand+"」で席を指定せずに入室してください", ctx)
			return nil
		}
		// その席が空いているか
		isOk, err := s.IfSeatAvailable(command.InOptions.SeatId, ctx)
		if err != nil {
			_ = s.LineBot.SendMessageWithError("failed s.IfSeatAvailable()", err)
			s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、エラーが発生しました。もう一度試してみてください", ctx)
			return err
		}
		if !isOk {
			s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、その番号の席は"+"今は使えません。他の空いている席を選ぶか、「"+InCommand+"」で席を指定せずに入室してください", ctx)
			return nil
		}
	}
	
	// すでに入室している場合
	isInRoom, err := s.IsUserInRoom(ctx)
	if err != nil {
		_ = s.LineBot.SendMessageWithError("failed s.IsUserInRoom()", err)
		return err
	}
	if isInRoom {
		// 現在座っている席を特定
		currentSeat, customErr := s.CurrentSeat(ctx)
		if customErr.IsNotNil() {
			_ = s.LineBot.SendMessageWithError("failed CurrentSeatId", customErr.Body)
			s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、エラーが発生しました", ctx)
			return customErr.Body
		}
		
		if command.CommandType == In { // !inの場合: 再度入室させる
			// 退室処理
			workedTimeSec, err := s.ExitRoom(currentSeat.SeatId, ctx)
			if err != nil {
				_ = s.LineBot.SendMessageWithError("failed in s.ExitRoom(seatId, ctx)", customErr.Body)
				s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、エラーが発生しました。もう一度試してみてください", ctx)
				return err
			}
			s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さんが再入室します"+
				"（+ "+strconv.Itoa(workedTimeSec/60)+"分）", ctx)
			// 入室処理: このまま次の処理に進む
		} else if command.CommandType == SeatIn {
			if command.InOptions.SeatId == currentSeat.SeatId { // 今と同じ席番号の場合、作業名と入室時間を更新
				// 作業名を更新
				err := s.FirestoreController.UpdateSeatWorkName(command.InOptions.WorkName, s.ProcessedUserId, ctx)
				if err != nil {
					_ = s.LineBot.SendMessageWithError("failed to UpdateSeatWorkName", err)
					s.SendLiveChatMessage(s.ProcessedUserDisplayName+
						"さん、エラーが発生しました。もう一度試してみてください", ctx)
					return err
				}
				// 入室時間を更新
				newUntil := utils.JstNow().Add(time.Duration(command.InOptions.WorkMin) * time.Minute)
				err = s.FirestoreController.UpdateSeatUntil(newUntil, s.ProcessedUserId, ctx)
				if err != nil {
					_ = s.LineBot.SendMessageWithError("failed to UpdateSeatUntil", err)
					s.SendLiveChatMessage(s.ProcessedUserDisplayName+
						"さん、エラーが発生しました。もう一度試してみてください", ctx)
					return err
				}
				
				s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さんはすでに"+strconv.Itoa(currentSeat.SeatId)+"番の席に座っています。作業名と入室時間を更新しました", ctx)
				return nil
			} else { // 今と別の席番号の場合: 退室させてから、入室させる。
				// 作業名は指定がない場合引き継ぐ。
				if command.InOptions.WorkName == "" && currentSeat.WorkName != "" {
					command.InOptions.WorkName = currentSeat.WorkName
				}
				
				// 退室処理
				workedTimeSec, err := s.ExitRoom(currentSeat.SeatId, ctx)
				if err != nil {
					_ = s.LineBot.SendMessageWithError("failed to ExitRoom for "+s.ProcessedUserId, err)
					s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、エラーが発生しました。もう一度試してみてください", ctx)
					return err
				}
				s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さんが席を移動します🚶（"+
					strconv.Itoa(currentSeat.SeatId)+"→"+strconv.Itoa(command.InOptions.SeatId)+"番席）"+
					"（+ "+strconv.Itoa(workedTimeSec/60)+"分）", ctx)
				
				// 入室処理: このまま次の処理に進む
			}
		}
	}
	
	// ここまで来ると入室処理は確定
	
	// 席を指定していない場合: 空いている席の番号をランダムに決定
	if command.CommandType == In {
		seatId, err := s.RandomAvailableSeatId(ctx)
		if err != nil {
			s.SendLiveChatMessage(s.ProcessedUserDisplayName+
				"さん、エラーが発生しました。もう一度試してみてください", ctx)
			return err
		}
		command.InOptions.SeatId = seatId
	}
	
	// ランクから席の色を決定
	var seatColorCode string
	userDoc, err := s.FirestoreController.RetrieveUser(s.ProcessedUserId, ctx)
	if err != nil {
		_ = s.LineBot.SendMessageWithError("failed to RetrieveUser", err)
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+
			"さん、エラーが発生しました。もう一度試してみてください", ctx)
		return err
	}
	if userDoc.RankVisible {
		rank, err := utils.GetRank(userDoc.TotalStudySec)
		if err != nil {
			_ = s.LineBot.SendMessageWithError("failed to GetRank", err)
			s.SendLiveChatMessage(s.ProcessedUserDisplayName+
				"さん、エラーが発生しました。もう一度試してみてください", ctx)
			return err
		}
		seatColorCode = rank.ColorCode
	} else {
		rank := utils.GetInvisibleRank()
		seatColorCode = rank.ColorCode
	}
	
	// 入室
	err = s.EnterRoom(command.InOptions.SeatId, command.InOptions.WorkName, command.InOptions.WorkMin, seatColorCode, ctx)
	if err != nil {
		_ = s.LineBot.SendMessageWithError("failed to enter room", err)
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+
			"さん、エラーが発生しました。もう一度試してみてください", ctx)
		return err
	}
	s.SendLiveChatMessage(s.ProcessedUserDisplayName+
		"さんが作業を始めました🔥（最大"+strconv.Itoa(command.InOptions.WorkMin)+"分、"+strconv.Itoa(command.InOptions.SeatId)+"番席）", ctx)
	
	return nil
}

func (s *System) Out(_ CommandDetails, ctx context.Context) error {
	// 今勉強中か？
	isInRoom, err := s.IsUserInRoom(ctx)
	if err != nil {
		_ = s.LineBot.SendMessageWithError("failed IsUserInRoom()", err)
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、エラーが発生しました。もう一度試してみてください", ctx)
		return err
	}
	if !isInRoom {
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、すでに退室しています", ctx)
		return nil
	}
	// 現在座っている席を特定
	seatId, customErr := s.CurrentSeatId(ctx)
	if customErr.Body != nil {
		_ = s.LineBot.SendMessageWithError("failed in s.CurrentSeatId(ctx)", customErr.Body)
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+
			"さん、残念ながらエラーが発生しました。もう一度試してみてください", ctx)
		return customErr.Body
	}
	// 退室処理
	workedTimeSec, err := s.ExitRoom(seatId, ctx)
	if err != nil {
		_ = s.LineBot.SendMessageWithError("failed in s.ExitRoom(seatId, ctx)", customErr.Body)
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、エラーが発生しました。もう一度試してみてください", ctx)
		return err
	} else {
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さんが退室しました🚶🚪"+
			"（+ "+strconv.Itoa(workedTimeSec/60)+"分、"+strconv.Itoa(seatId)+"番席）", ctx)
		return nil
	}
}

func (s *System) ShowUserInfo(command CommandDetails, ctx context.Context) error {
	// そのユーザーはドキュメントがあるか？
	isUserRegistered, err := s.IfUserRegistered(ctx)
	if err != nil {
		return err
	}
	if isUserRegistered {
		liveChatMessage := ""
		totalTimeStr, dailyTotalTimeStr, err := s.TotalStudyTimeStrings(ctx)
		if err != nil {
			_ = s.LineBot.SendMessageWithError("failed s.TotalStudyTimeStrings()", err)
			return err
		}
		liveChatMessage += s.ProcessedUserDisplayName +
			"さん　［本日の作業時間：" + dailyTotalTimeStr + "］" +
			" ［累計作業時間：" + totalTimeStr + "］"
		
		if command.InfoOption.ShowDetails {
			userDoc, err := s.FirestoreController.RetrieveUser(s.ProcessedUserId, ctx)
			if err != nil {
				_ = s.LineBot.SendMessageWithError("failed s.FirestoreController.RetrieveUser", err)
				return err
			}
			
			switch userDoc.RankVisible {
			case true:
				liveChatMessage += "［ランク表示：オン］"
			case false:
				liveChatMessage += "［ランク表示：オフ］"
			}
			
			liveChatMessage += "［登録日：" + userDoc.RegistrationDate.Format("2006年01月02日") + "］"
		}
		s.SendLiveChatMessage(liveChatMessage, ctx)
	} else {
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+
			"さんはまだ作業データがありません。「"+InCommand+"」コマンドで作業を始めましょう！", ctx)
	}
	return nil
}

func (s *System) ShowSeatInfo(_ CommandDetails, ctx context.Context) error {
	// そのユーザーは入室しているか？
	isUserInRoom, err := s.IsUserInRoom(ctx)
	if err != nil {
		return err
	}
	if isUserInRoom {
		currentSeat, err := s.CurrentSeat(ctx)
		if err.IsNotNil() {
			s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、エラーが発生しました。もう一度試してみてください", ctx)
			_ = s.LineBot.SendMessageWithError("failed s.CurrentSeat()", err.Body)
		}
		
		realtimeWorkedTimeMin := int(utils.JstNow().Sub(currentSeat.EnteredAt).Minutes())
		remainingMinutes := int(currentSeat.Until.Sub(utils.JstNow()).Minutes())
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さんは"+strconv.Itoa(currentSeat.SeatId)+"番の席に座っています。現在"+strconv.Itoa(realtimeWorkedTimeMin)+"分入室中。自動退室まで残り"+strconv.Itoa(remainingMinutes)+"分です", ctx)
	} else {
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+
			"さんは入室していません。「"+InCommand+"」コマンドで入室しましょう！", ctx)
	}
	return nil
}

func (s *System) Report(command CommandDetails, ctx context.Context) error {
	if command.ReportMessage == "" { // !reportのみは不可
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、スペースを空けてメッセージを書いてください。", ctx)
		return nil
	}
	
	lineMessage := "【" + ReportCommand + "受信】\n" +
		"チャンネルID: " + s.ProcessedUserId + "\n" +
		"チャンネル名: " + s.ProcessedUserDisplayName + "\n\n" +
		command.ReportMessage
	err := s.LineBot.SendMessage(lineMessage)
	if err != nil {
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、エラーが発生しました", ctx)
		log.Println(err)
	}
	
	discordMessage := "【" + ReportCommand + "受信】\n" +
		"チャンネル名: `" + s.ProcessedUserDisplayName + "`\n" +
		"メッセージ: `" + command.ReportMessage + "`"
	err = s.DiscordBot.SendMessage(discordMessage)
	if err != nil {
		_ = s.LineBot.SendMessageWithError("discordへメッセージが送信できませんでした: \""+discordMessage+"\"", err)
	}
	
	s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、管理者へメッセージを送信しました", ctx)
	return nil
}

func (s *System) Kick(command CommandDetails, ctx context.Context) error {
	// commanderはモデレーターかチャットオーナーか
	if s.ProcessedUserIsModeratorOrOwner {
		// ターゲットの座席は誰か使っているか
		isSeatAvailable, err := s.IfSeatAvailable(command.KickSeatId, ctx)
		if err != nil {
			return err
		}
		if !isSeatAvailable {
			// ユーザーを強制退室させる
			seat, cerr := s.RetrieveSeatBySeatId(command.KickSeatId, ctx)
			if cerr.IsNotNil() {
				return cerr.Body
			}
			s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、"+strconv.Itoa(seat.SeatId)+"番席の"+seat.UserDisplayName+"さんを退室させます", ctx)
			
			s.SetProcessedUser(seat.UserId, seat.UserDisplayName, false, false)
			outCommandDetails := CommandDetails{
				CommandType: Out,
				InOptions:   InOptions{},
			}
			
			err := s.Out(outCommandDetails, ctx)
			if err != nil {
				return err
			}
		} else {
			s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、その番号の座席は誰も使用していません", ctx)
		}
	} else {
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さんは「"+KickCommand+"」コマンドを使用できません", ctx)
	}
	return nil
}

func (s *System) Check(command CommandDetails, ctx context.Context) error {
	// commanderはモデレーターかチャットオーナーか
	if s.ProcessedUserIsModeratorOrOwner {
		// ターゲットの座席は誰か使っているか
		isSeatAvailable, err := s.IfSeatAvailable(command.CheckSeatId, ctx)
		if err != nil {
			return err
		}
		if !isSeatAvailable {
			// 座席情報を表示する
			seat, cerr := s.RetrieveSeatBySeatId(command.CheckSeatId, ctx)
			if cerr.IsNotNil() {
				return cerr.Body
			}
			sinceMinutes := utils.JstNow().Sub(seat.EnteredAt).Minutes()
			untilMinutes := seat.Until.Sub(utils.JstNow()).Minutes()
			message := s.ProcessedUserDisplayName + "さん、" + strconv.Itoa(seat.SeatId) + "番席には" +
				seat.UserDisplayName + "さんが" + strconv.Itoa(int(sinceMinutes)) + "分間着席しており、" +
				"作業名は\"" + seat.WorkName + "\"です。" + strconv.Itoa(int(untilMinutes)) + "分後に自動退室予定です。"
			s.SendLiveChatMessage(message, ctx)
		} else {
			s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、その番号の座席は誰も使用していません", ctx)
		}
	} else {
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さんは「"+CheckCommand+"」コマンドを使用できません", ctx)
	}
	return nil
}

func (s *System) My(command CommandDetails, ctx context.Context) error {
	// ユーザードキュメントはすでにあり、登録されていないプロパティだった場合、そのままプロパティを保存したら自動で作成される。
	// また、読み込みのときにそのプロパティがなくても大丈夫。自動で初期値が割り当てられる。
	// ただし、ユーザードキュメントがそもそもない場合は、書き込んでもエラーにはならないが、登録日が記録されないため、要登録。
	
	// そのユーザーはドキュメントがあるか？
	isUserRegistered, err := s.IfUserRegistered(ctx)
	if err != nil {
		return err
	}
	if !isUserRegistered { // ない場合は作成。
		err := s.InitializeUser(ctx)
		if err != nil {
			return err
		}
	}
	
	// オプションが1つ以上指定されているか？
	if len(command.MyOptions) == 0 {
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、オプションが正しく設定されているか確認してください", ctx)
		return nil
	}
	
	for _, myOption := range command.MyOptions {
		if myOption.Type == RankVisible {
			userDoc, err := s.FirestoreController.RetrieveUser(s.ProcessedUserId, ctx)
			if err != nil {
				_ = s.LineBot.SendMessageWithError("faield  s.FirestoreController.RetrieveUser()", err)
				s.SendLiveChatMessage(s.ProcessedUserDisplayName+
					"さん、エラーが発生しました。もう一度試してみてください", ctx)
				return err
			}
			// 現在の値と、設定したい値が同じなら、変更なし
			if userDoc.RankVisible == myOption.BoolValue {
				var rankVisibleString string
				if userDoc.RankVisible {
					rankVisibleString = "オン"
				} else {
					rankVisibleString = "オフ"
				}
				s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さんのランク表示モードはすでに"+rankVisibleString+"です", ctx)
			} else {
				// 違うなら、切替
				err := s.ToggleRankVisible(ctx)
				if err != nil {
					_ = s.LineBot.SendMessageWithError("failed to ToggleRankVisible", err)
					s.SendLiveChatMessage(s.ProcessedUserDisplayName+
						"さん、エラーが発生しました。もう一度試してみてください", ctx)
					return err
				}
			}
		}
		if myOption.Type == DefaultStudyMin {
			err := s.FirestoreController.SetMyDefaultStudyMin(s.ProcessedUserId, myOption.IntValue, ctx)
			if err != nil {
				_ = s.LineBot.SendMessageWithError("failed to set my-default-study-min", err)
				s.SendLiveChatMessage(s.ProcessedUserDisplayName+
					"さん、エラーが発生しました。もう一度試してみてください", ctx)
				return err
			}
			s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さんのデフォルトの作業時間を"+strconv.Itoa(myOption.IntValue)+"分に設定しました", ctx)
		}
	}
	return nil
}

func (s *System) Change(command CommandDetails, ctx context.Context) error {
	// そのユーザーは入室中か？
	isUserInRoom, err := s.IsUserInRoom(ctx)
	if err != nil {
		return err
	}
	if !isUserInRoom {
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、入室中のみ使えるコマンドです", ctx)
		return nil
	}
	currentSeatId, customErr := s.CurrentSeatId(ctx)
	if customErr.IsNotNil() {
		_ = s.LineBot.SendMessageWithError("failed CurrentSeatId", customErr.Body)
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、エラーが発生しました", ctx)
		return customErr.Body
	}
	
	// オプションが1つ以上指定されているか？
	if len(command.ChangeOptions) == 0 {
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、オプションが正しく設定されているか確認してください", ctx)
		return nil
	}
	
	for _, changeOption := range command.ChangeOptions {
		if changeOption.Type == WorkName {
			// 作業名を書きかえ
			err := s.FirestoreController.UpdateSeatWorkName(changeOption.StringValue, s.ProcessedUserId, ctx)
			if err != nil {
				_ = s.LineBot.SendMessageWithError("failed to UpdateWorkName", err)
				s.SendLiveChatMessage(s.ProcessedUserDisplayName+
					"さん、エラーが発生しました。もう一度試してみてください", ctx)
				return err
			}
			s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さんの作業名を更新しました（"+strconv.Itoa(currentSeatId)+"番席）", ctx)
		}
		if changeOption.Type == WorkTime {
			// 作業時間（入室時間から自動退室までの時間）を変更
			currentSeat, cerr := s.CurrentSeat(ctx)
			if cerr.IsNotNil() {
				_ = s.LineBot.SendMessageWithError("failed to s.CurrentSeat(ctx)", cerr.Body)
				s.SendLiveChatMessage(s.ProcessedUserDisplayName+
					"さん、エラーが発生しました。もう一度試してみてください", ctx)
				return cerr.Body
			}
			realtimeWorkedTimeMin := int(utils.JstNow().Sub(currentSeat.EnteredAt).Minutes())
			
			requestedUntil := currentSeat.EnteredAt.Add(time.Duration(changeOption.IntValue) * time.Minute)
			
			if requestedUntil.Before(utils.JstNow()) { // もし現在時刻で指定時間よりも経過していたら却下
				remainingWorkMin := int(currentSeat.Until.Sub(utils.JstNow()).Minutes())
				s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、すでに"+strconv.Itoa(changeOption.IntValue)+"分以上入室しています。現在"+strconv.Itoa(realtimeWorkedTimeMin)+"分入室中。自動退室まで残り"+strconv.Itoa(remainingWorkMin)+"分です", ctx)
			} else if requestedUntil.After(utils.JstNow().Add(time.Duration(s.MaxWorkTimeMin) * time.Minute)) { // もし現在時刻より最大延長可能時間以上後なら却下
				remainingWorkMin := int(currentSeat.Until.Sub(utils.JstNow()).Minutes())
				s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、自動退室までの時間は現在時刻から"+strconv.Itoa(s.MaxWorkTimeMin)+"分後まで設定できます。現在"+strconv.Itoa(realtimeWorkedTimeMin)+"分入室中。自動退室まで残り"+strconv.Itoa(remainingWorkMin)+"分です", ctx)
			} else { // それ以外なら延長
				err := s.FirestoreController.UpdateSeatUntil(requestedUntil, s.ProcessedUserId, ctx)
				if err != nil {
					_ = s.LineBot.SendMessageWithError("failed to s.FirestoreController.UpdateSeatUntil", cerr.Body)
					s.SendLiveChatMessage(s.ProcessedUserDisplayName+
						"さん、エラーが発生しました。もう一度試してみてください", ctx)
					return err
				}
				remainingWorkMin := int(requestedUntil.Sub(utils.JstNow()).Minutes())
				s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、入室時間を"+strconv.Itoa(changeOption.IntValue)+"分に変更しました。現在"+strconv.Itoa(realtimeWorkedTimeMin)+"分入室中。自動退室まで残り"+strconv.Itoa(remainingWorkMin)+"分です", ctx)
			}
		}
	}
	return nil
}

func (s *System) More(command CommandDetails, ctx context.Context) error {
	// 入室しているか？
	isUserInRoom, err := s.IsUserInRoom(ctx)
	if err != nil {
		return err
	}
	if isUserInRoom {
		// 時間を指定分延長
		currentSeat, cerr := s.CurrentSeat(ctx)
		if cerr.IsNotNil() {
			return cerr.Body
		}
		newUntil := currentSeat.Until.Add(time.Duration(command.MoreMinutes) * time.Minute)
		// もし延長後の時間が最大作業時間を超えていたら、最大作業時間まで延長
		if int(newUntil.Sub(utils.JstNow()).Minutes()) > s.MaxWorkTimeMin {
			newUntil = utils.JstNow().Add(time.Duration(s.MaxWorkTimeMin) * time.Minute)
			s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、現在時刻から"+
				strconv.Itoa(s.MaxWorkTimeMin)+"分後までのみ作業時間を延長することができます。延長できる最大の時間で設定します", ctx)
		}
		
		err := s.FirestoreController.UpdateSeatUntil(newUntil, s.ProcessedUserId, ctx)
		if err != nil {
			_ = s.LineBot.SendMessageWithError("failed to s.FirestoreController.UpdateSeatUntil", cerr.Body)
			s.SendLiveChatMessage(s.ProcessedUserDisplayName+
				"さん、エラーが発生しました。もう一度試してみてください", ctx)
			return err
		}
		addedMin := int(newUntil.Sub(currentSeat.Until).Minutes())
		realtimeWorkedTimeMin := int(utils.JstNow().Sub(currentSeat.EnteredAt).Minutes())
		remainingWorkMin := int(newUntil.Sub(utils.JstNow()).Minutes())
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、自動退室までの時間を"+strconv.Itoa(addedMin)+"分延長しました。現在"+strconv.Itoa(realtimeWorkedTimeMin)+"分入室中。自動退室まで残り"+strconv.Itoa(remainingWorkMin)+"分です", ctx)
	} else {
		s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、入室中のみ使えるコマンドです", ctx)
	}
	
	return nil
}

func (s *System) Rank(_ CommandDetails, ctx context.Context) error {
	// そのユーザーはドキュメントがあるか？
	isUserRegistered, err := s.IfUserRegistered(ctx)
	if err != nil {
		return err
	}
	if !isUserRegistered { // ない場合は作成。
		err := s.InitializeUser(ctx)
		if err != nil {
			return err
		}
	}
	
	// ランク表示設定のON/OFFを切り替える
	err = s.ToggleRankVisible(ctx)
	if err != nil {
		return err
	}
	return nil
}

func (s *System) ToggleRankVisible(ctx context.Context) error {
	// TODO: 入室中にランクアップしても、新しい色が反映されるようにする
	// get current value
	userDoc, err := s.FirestoreController.RetrieveUser(s.ProcessedUserId, ctx)
	if err != nil {
		return err
	}
	currentRankVisible := userDoc.RankVisible
	newRankVisible := !currentRankVisible
	
	// set reverse value
	err = s.FirestoreController.SetMyRankVisible(s.ProcessedUserId, newRankVisible, ctx)
	if err != nil {
		return err
	}
	
	var newValueString string
	if newRankVisible {
		newValueString = "オン"
	} else {
		newValueString = "オフ"
	}
	s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さんのランク表示を"+newValueString+"にしました", ctx)
	
	// 入室中であれば、座席の色も変える
	isUserInRoom, err := s.IsUserInRoom(ctx)
	if isUserInRoom {
		var rank utils.Rank
		if newRankVisible { // ランクから席の色を取得
			rank, err = utils.GetRank(userDoc.TotalStudySec)
			if err != nil {
				_ = s.LineBot.SendMessageWithError("failed to GetRank", err)
				s.SendLiveChatMessage(s.ProcessedUserDisplayName+
					"さん、エラーが発生しました。もう一度試してみてください", ctx)
				return err
			}
		} else { // ランク表示オフの色を取得
			rank = utils.GetInvisibleRank()
		}
		// 席の色を更新
		err := s.FirestoreController.UpdateSeatColorCode(rank.ColorCode, s.ProcessedUserId, ctx)
		if err != nil {
			_ = s.LineBot.SendMessageWithError("failed to s.FirestoreController.UpdateSeatColorCode()", err)
			s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さん、エラーが発生しました。もう一度試してください", ctx)
			return err
		}
	}
	
	return nil
}

// IsSeatExist 席番号1～max-seatsの席かどうかを判定。
func (s *System) IsSeatExist(seatId int, ctx context.Context) (bool, error) {
	constants, err := s.FirestoreController.RetrieveSystemConstantsConfig(ctx)
	if err != nil {
		return false, err
	}
	return 1 <= seatId && seatId <= constants.MaxSeats, nil
}

// IfSeatAvailable 席番号がseatIdの席が空いているかどうか。
func (s *System) IfSeatAvailable(seatId int, ctx context.Context) (bool, error) {
	// 使われているかどうか
	roomData, err := s.FirestoreController.RetrieveRoom(ctx)
	if err != nil {
		return false, err
	}
	for _, seat := range roomData.Seats {
		if seat.SeatId == seatId {
			return false, nil
		}
	}
	// ここまで来ると指定された番号の席が使われていないということ
	
	// 存在するかどうか
	isExist, err := s.IsSeatExist(seatId, ctx)
	if err != nil {
		return false, err
	}
	
	return isExist, nil
}

func (s *System) RetrieveSeatBySeatId(seatId int, ctx context.Context) (myfirestore.Seat, customerror.CustomError) {
	roomDoc, err := s.FirestoreController.RetrieveRoom(ctx)
	if err != nil {
		return myfirestore.Seat{}, customerror.Unknown.Wrap(err)
	}
	for _, seat := range roomDoc.Seats {
		if seat.SeatId == seatId {
			return seat, customerror.NewNil()
		}
	}
	// ここまで来ると指定された番号の席が使われていないということ
	return myfirestore.Seat{}, customerror.SeatNotFound.New("that seat is not used.")
}

func (s *System) IfUserRegistered(ctx context.Context) (bool, error) {
	_, err := s.FirestoreController.RetrieveUser(s.ProcessedUserId, ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return false, nil
		} else {
			return false, err
		}
	}
	return true, nil
}

// IsUserInRoom そのユーザーがルーム内にいるか？登録済みかに関わらず。
func (s *System) IsUserInRoom(ctx context.Context) (bool, error) {
	roomData, err := s.FirestoreController.RetrieveRoom(ctx)
	if err != nil {
		return false, err
	}
	for _, seat := range roomData.Seats {
		if seat.UserId == s.ProcessedUserId {
			return true, nil
		}
	}
	return false, nil
}

func (s *System) InitializeUser(ctx context.Context) error {
	log.Println("InitializeUser()")
	userData := myfirestore.UserDoc{
		DailyTotalStudySec: 0,
		TotalStudySec:      0,
		RegistrationDate:   utils.JstNow(),
	}
	return s.FirestoreController.InitializeUser(s.ProcessedUserId, userData, ctx)
}

func (s *System) RetrieveNextPageToken(ctx context.Context) (string, error) {
	return s.FirestoreController.RetrieveNextPageToken(ctx)
}

func (s *System) SaveNextPageToken(nextPageToken string, ctx context.Context) error {
	return s.FirestoreController.SaveNextPageToken(nextPageToken, ctx)
}

// RandomAvailableSeatId roomの席が空いているならその中からランダムな席番号を、空いていないならmax-seatsを増やし、最小の空席番号を返す。
func (s *System) RandomAvailableSeatId(ctx context.Context) (int, error) {
	room, err := s.FirestoreController.RetrieveRoom(ctx)
	if err != nil {
		return 0, err
	}
	
	constants, err := s.FirestoreController.RetrieveSystemConstantsConfig(ctx)
	if err != nil {
		return 0, err
	}
	
	var availableSeatIdList []int
	for id := 1; id <= constants.MaxSeats; id++ {
		isUsed := false
		for _, seatInUse := range room.Seats {
			if id == seatInUse.SeatId {
				isUsed = true
				break
			}
		}
		if !isUsed {
			availableSeatIdList = append(availableSeatIdList, id)
		}
	}
	
	if len(availableSeatIdList) > 0 {
		rand.Seed(utils.JstNow().UnixNano())
		return availableSeatIdList[rand.Intn(len(availableSeatIdList))], nil
	} else { // max-seatsが足りない
		// 設定されている空席率を満たすような値を求める
		newMaxSeats := int(math.Ceil(float64(float32(constants.MaxSeats) / constants.MinVacancyRate)))
		err := s.FirestoreController.SetMaxSeats(newMaxSeats, ctx)
		if err != nil {
			return 0, err
		}
		if newMaxSeats <= constants.MaxSeats {
			_ = s.LineBot.SendMessage("newMaxSeats: 設定されている空席率を満たすような値を求めることができませんでした")
			return 0, errors.New("エラーが発生しました。")
		}
		return constants.MaxSeats + 1, nil
	}
}

// EnterRoom 入室させる。事前チェックはされている前提。
func (s *System) EnterRoom(seatId int, workName string, workTimeMin int, seatColorCode string, ctx context.Context) error {
	enterDate := utils.JstNow()
	exitDate := enterDate.Add(time.Duration(workTimeMin) * time.Minute)
	seat, err := s.FirestoreController.SetSeat(seatId, workName, enterDate, exitDate, seatColorCode, s.ProcessedUserId, s.ProcessedUserDisplayName, ctx)
	if err != nil {
		return err
	}
	// 入室時刻を記録
	err = s.FirestoreController.SetLastEnteredDate(s.ProcessedUserId, enterDate, ctx)
	if err != nil {
		_ = s.LineBot.SendMessageWithError("failed to set last entered date", err)
		return err
	}
	// ログ記録
	err = s.FirestoreController.AddUserHistory(s.ProcessedUserId, EnterAction, seat, ctx)
	if err != nil {
		return err
	}
	return nil
}

// ExitRoom ユーザーを退室させる。事前チェックはされている前提。
func (s *System) ExitRoom(seatId int, ctx context.Context) (int, error) {
	var seat myfirestore.Seat
	room, err := s.FirestoreController.RetrieveRoom(ctx)
	if err != nil {
		return 0, err
	}
	for _, seatInRoom := range room.Seats {
		if seatInRoom.UserId == s.ProcessedUserId {
			seat = seatInRoom
		}
	}
	if seat.UserId == "" {
		message := "指定されたuserId (" + s.ProcessedUserId + ", " + s.ProcessedUserDisplayName + ") の人は今入室していない。"
		_ = s.LineBot.SendMessage(message)
		return 0, errors.New(message)
	}
	
	// 作業時間を計算
	exitDate := utils.JstNow()
	workedTimeSec := int(exitDate.Sub(seat.EnteredAt).Seconds())
	var dailyWorkedTimeSec int
	// もし日付変更を跨いで入室してたら、当日の累計時間は日付変更からの時間にする
	if workedTimeSec > utils.InSeconds(exitDate) {
		dailyWorkedTimeSec = utils.InSeconds(exitDate)
	} else {
		dailyWorkedTimeSec = workedTimeSec
	}
	
	err = s.FirestoreController.UnSetSeatInRoom(seat, ctx)
	if err != nil {
		return 0, err
	}
	// ログ記録
	err = s.FirestoreController.AddUserHistory(s.ProcessedUserId, ExitAction, seat, ctx)
	if err != nil {
		_ = s.LineBot.SendMessageWithError("failed to add an user history", err)
	}
	// 退室時刻を記録
	err = s.FirestoreController.SetLastExitedDate(s.ProcessedUserId, exitDate, ctx)
	if err != nil {
		_ = s.LineBot.SendMessageWithError("failed to update last-exited-date", err)
		return 0, err
	}
	// 累計学習時間を更新
	err = s.UpdateTotalWorkTime(workedTimeSec, dailyWorkedTimeSec, ctx)
	if err != nil {
		_ = s.LineBot.SendMessageWithError("failed to update total study time", err)
		return 0, err
	}
	
	log.Println(s.ProcessedUserId + " exited the room. seat id: " + strconv.Itoa(seatId) + " (+ " + strconv.Itoa(workedTimeSec) + "秒)")
	return workedTimeSec, nil
}

func (s *System) CurrentSeatId(ctx context.Context) (int, customerror.CustomError) {
	currentSeat, err := s.CurrentSeat(ctx)
	if err.IsNotNil() {
		return -1, err
	}
	return currentSeat.SeatId, customerror.NewNil()
}

func (s *System) CurrentSeat(ctx context.Context) (myfirestore.Seat, customerror.CustomError) {
	roomData, err := s.FirestoreController.RetrieveRoom(ctx)
	if err != nil {
		return myfirestore.Seat{}, customerror.Unknown.Wrap(err)
	}
	for _, seat := range roomData.Seats {
		if seat.UserId == s.ProcessedUserId {
			return seat, customerror.NewNil()
		}
	}
	// 入室していない
	return myfirestore.Seat{}, customerror.UserNotInAnyRoom.New("the user is not in any room.")
}

func (s *System) UpdateTotalWorkTime(workedTimeSec int, dailyWorkedTimeSec int, ctx context.Context) error {
	userData, err := s.FirestoreController.RetrieveUser(s.ProcessedUserId, ctx)
	if err != nil {
		return err
	}
	// 更新前の値
	previousTotalSec := userData.TotalStudySec
	previousDailyTotalSec := userData.DailyTotalStudySec
	// 更新後の値
	newTotalSec := previousTotalSec + workedTimeSec
	newDailyTotalSec := previousDailyTotalSec + dailyWorkedTimeSec
	
	// 累計作業時間が減るなんてことがないか確認
	if newTotalSec < previousTotalSec {
		message := "newTotalSec < previousTotalSec ??!! 処理を中断します。"
		_ = s.LineBot.SendMessage(s.ProcessedUserId + " (" + s.ProcessedUserDisplayName + ") " + message)
		return errors.New(message)
	}
	
	err = s.FirestoreController.UpdateTotalTime(s.ProcessedUserId, newTotalSec, newDailyTotalSec, ctx)
	if err != nil {
		return err
	}
	return nil
}

// TotalStudyTimeStrings リアルタイムの累積作業時間・当日累積作業時間を文字列で返す。
func (s *System) TotalStudyTimeStrings(ctx context.Context) (string, string, error) {
	// 入室中ならばリアルタイムの作業時間も加算する
	realtimeDuration := time.Duration(0)
	realtimeDailyDuration := time.Duration(0)
	if isInRoom, _ := s.IsUserInRoom(ctx); isInRoom {
		// 作業時間を計算
		jstNow := utils.JstNow()
		currentSeat, err := s.CurrentSeat(ctx)
		if err.IsNotNil() {
			return "", "", err.Body
		}
		workedTimeSec := int(jstNow.Sub(currentSeat.EnteredAt).Seconds())
		realtimeDuration = time.Duration(workedTimeSec) * time.Second
		
		var dailyWorkedTimeSec int
		if workedTimeSec > utils.InSeconds(jstNow) {
			dailyWorkedTimeSec = utils.InSeconds(jstNow)
		} else {
			dailyWorkedTimeSec = workedTimeSec
		}
		realtimeDailyDuration = time.Duration(dailyWorkedTimeSec) * time.Second
	}
	
	userData, err := s.FirestoreController.RetrieveUser(s.ProcessedUserId, ctx)
	if err != nil {
		return "", "", err
	}
	// 累計
	var totalStr string
	totalDuration := realtimeDuration + time.Duration(userData.TotalStudySec)*time.Second
	if totalDuration < time.Hour {
		totalStr = strconv.Itoa(int(totalDuration.Minutes())) + "分"
	} else {
		totalStr = strconv.Itoa(int(totalDuration.Hours())) + "時間" +
			strconv.Itoa(int(totalDuration.Minutes())%60) + "分"
	}
	// 当日の累計
	var dailyTotalStr string
	dailyTotalDuration := realtimeDailyDuration + time.Duration(userData.DailyTotalStudySec)*time.Second
	if dailyTotalDuration < time.Hour {
		dailyTotalStr = strconv.Itoa(int(dailyTotalDuration.Minutes())) + "分"
	} else {
		dailyTotalStr = strconv.Itoa(int(dailyTotalDuration.Hours())) + "時間" +
			strconv.Itoa(int(dailyTotalDuration.Minutes())%60) + "分"
	}
	return totalStr, dailyTotalStr, nil
}

// ExitAllUserInRoom roomの全てのユーザーを退室させる。
func (s *System) ExitAllUserInRoom(ctx context.Context) error {
	room, err := s.FirestoreController.RetrieveRoom(ctx)
	if err != nil {
		return err
	}
	for _, seat := range room.Seats {
		s.SetProcessedUser(seat.UserId, seat.UserDisplayName, false, false)
		_, err := s.ExitRoom(seat.SeatId, ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *System) SendLiveChatMessage(message string, ctx context.Context) {
	err := s.LiveChatBot.PostMessage(message, ctx)
	if err != nil {
		_ = s.LineBot.SendMessageWithError("failed to send live chat message", err)
	}
	return
}

// OrganizeDatabase untilを過ぎているルーム内のユーザーを退室させる。
func (s *System) OrganizeDatabase(ctx context.Context) error {
	room, err := s.FirestoreController.RetrieveRoom(ctx)
	if err != nil {
		return err
	}
	for _, seat := range room.Seats {
		if seat.Until.Before(utils.JstNow()) {
			s.SetProcessedUser(seat.UserId, seat.UserDisplayName, false, false)
			
			workedTimeSec, err := s.ExitRoom(seat.SeatId, ctx)
			if err != nil {
				_ = s.LineBot.SendMessageWithError(s.ProcessedUserDisplayName+"さん（"+s.ProcessedUserId+"）の退室処理中にエラーが発生しました", err)
				// !outとバッティングしたときにここに来るが、止めることではない
			} else {
				s.SendLiveChatMessage(s.ProcessedUserDisplayName+"さんが退室しました🚶🚪"+
					"（+ "+strconv.Itoa(workedTimeSec/60)+"分、"+strconv.Itoa(seat.SeatId)+"番席）", ctx)
			}
		}
	}
	return nil
}

func (s *System) CheckLiveStreamStatus(ctx context.Context) error {
	checker := guardians.NewLiveStreamChecker(s.FirestoreController, s.LiveChatBot, s.LineBot)
	return checker.Check(ctx)
}

func (s *System) ResetDailyTotalStudyTime(ctx context.Context) error {
	log.Println("ResetDailyTotalStudyTime()")
	constantsConfig, err := s.FirestoreController.RetrieveSystemConstantsConfig(ctx)
	if err != nil {
		return err
	}
	previousDate := constantsConfig.LastResetDailyTotalStudySec.In(utils.JapanLocation())
	now := utils.JstNow()
	isDifferentDay := now.Year() != previousDate.Year() || now.Month() != previousDate.Month() || now.Day() != previousDate.Day()
	if isDifferentDay && now.After(previousDate) {
		userIter := s.FirestoreController.RetrieveAllNonDailyZeroUserDocs(ctx)
		if err != nil {
			return err
		}
		count := 0
		for {
			doc, err := userIter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return err
			}
			err = s.FirestoreController.ResetDailyTotalStudyTime(doc.Ref, ctx)
			if err != nil {
				return err
			}
			count += 1
		}
		_ = s.LineBot.SendMessage("successfully reset all non-daily-zero user's daily total study time. (" + strconv.Itoa(count) + " users)")
		err = s.FirestoreController.SetLastResetDailyTotalStudyTime(now, ctx)
		if err != nil {
			return err
		}
	} else {
		_ = s.LineBot.SendMessage("all user's daily total study times are already reset today.")
	}
	return nil
}

func (s *System) RetrieveAllUsersTotalStudySecList(ctx context.Context) ([]UserIdTotalStudySecSet, error) {
	var set []UserIdTotalStudySecSet
	
	userDocRefs, err := s.FirestoreController.RetrieveAllUserDocRefs(ctx)
	if err != nil {
		return set, err
	}
	for _, userDocRef := range userDocRefs {
		userDoc, err := s.FirestoreController.RetrieveUser(userDocRef.ID, ctx)
		if err != nil {
			return set, err
		}
		set = append(set, UserIdTotalStudySecSet{
			UserId:        userDocRef.ID,
			TotalStudySec: userDoc.TotalStudySec,
		})
	}
	return set, nil
}

// MinAvailableSeatId 空いている最小の番号の席番号を求める
func (s *System) MinAvailableSeatId(ctx context.Context) (int, error) {
	roomDoc, err := s.FirestoreController.RetrieveRoom(ctx)
	if err != nil {
		return -1, err
	}
	
	if len(roomDoc.Seats) > 0 {
		// 使用されている座席番号リストを取得
		var usedSeatIds []int
		for _, seat := range roomDoc.Seats {
			usedSeatIds = append(usedSeatIds, seat.SeatId)
		}
		
		// 使用されていない最小の席番号を求める。1から順に探索
		searchingSeatId := 1
		for {
			// searchingSeatIdがusedSeatIdsに含まれているか
			isUsed := false
			for _, usedSeatId := range usedSeatIds {
				if usedSeatId == searchingSeatId {
					isUsed = true
				}
			}
			if !isUsed { // 使われていなければその席番号を返す
				return searchingSeatId, nil
			}
			searchingSeatId += 1
		}
	} else { // 誰も入室していない場合
		return 1, nil
	}
}
