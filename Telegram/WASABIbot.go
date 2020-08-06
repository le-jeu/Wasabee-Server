package wasabeetelegram

import (
	// "encoding/json"
	"fmt"
	"html/template"
	"strconv"
	"strings"

	"github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/wasabee-project/Wasabee-Server"
)

// TGConfiguration is the main configuration data for the Telegram interface
// passed to main() pre-loaded with APIKey and TemplateSet set, the rest is built when the bot starts
type TGConfiguration struct {
	APIKey      string
	HookPath    string
	TemplateSet map[string]*template.Template
	baseKbd     tgbotapi.ReplyKeyboardMarkup
	upChan      chan tgbotapi.Update
	hook        string
}

var bot *tgbotapi.BotAPI
var config TGConfiguration

// WasabeeBot is called from main() to start the bot.
func WasabeeBot(init TGConfiguration) {
	if init.APIKey == "" {
		wasabee.Log.Infow("startup", "subsystem", "Telegram", "message", "Telegram API key not set; not starting")
		return
	}
	config.APIKey = init.APIKey

	if init.TemplateSet == nil {
		wasabee.Log.Warnw("startup", "subsystem", "Telegram", "message", "the UI templates are not loaded; not starting Telegram bot")
		return
	}
	config.TemplateSet = init.TemplateSet

	keyboards(&config)

	config.HookPath = init.HookPath
	if config.HookPath == "" {
		config.HookPath = "/tg"
	}

	config.upChan = make(chan tgbotapi.Update, 10) // not using bot.ListenForWebhook() since we need our own bidirectional channel
	webhook := wasabee.Subrouter(config.HookPath)
	webhook.HandleFunc("/{hook}", TGWebHook).Methods("POST")

	_ = wasabee.RegisterMessageBus("Telegram", SendMessage)

	var err error
	bot, err = tgbotapi.NewBotAPI(config.APIKey)
	if err != nil {
		wasabee.Log.Error(err)
		return
	}

	// bot.Debug = true
	wasabee.Log.Infow("startup", "subsystem", "Telegram", "message", "authorized to Telegram as "+bot.Self.UserName)
	wasabee.TGSetBot(bot.Self.UserName, bot.Self.ID)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	webroot, _ := wasabee.GetWebroot()
	config.hook = wasabee.GenerateName()
	t := fmt.Sprintf("%s%s/%s", webroot, config.HookPath, config.hook)
	_, err = bot.SetWebhook(tgbotapi.NewWebhook(t))
	if err != nil {
		wasabee.Log.Error(err)
		return
	}

	i := 1
	for update := range config.upChan {
		// wasabee.Log.Debugf("running update: %s", update)
		if err = runUpdate(update); err != nil {
			wasabee.Log.Error(err)
			continue
		}
		if (i % 100) == 0 { // every 100 requests, change the endpoint; I'm _not_ paranoid.
			i = 1
			config.hook = wasabee.GenerateName()
			t = fmt.Sprintf("%s%s/%s", webroot, config.HookPath, config.hook)
			_, err = bot.SetWebhook(tgbotapi.NewWebhook(t))
			if err != nil {
				wasabee.Log.Error(err)
			}
		}
		i++
	}
}

// Shutdown closes all the Telegram connections
// called only at server shutdown
func Shutdown() {
	wasabee.Log.Info("shutdown", "subsystem", "Telegram")
	_, _ = bot.RemoveWebhook()
	bot.StopReceivingUpdates()
}

func runUpdate(update tgbotapi.Update) error {
	if update.CallbackQuery != nil {
		wasabee.Log.Debugw("callback", "subsystem", "Telegram", "data", update)
		msg, err := callback(&update)
		if err != nil {
			wasabee.Log.Error(err)
			return err
		}
		if _, err = bot.Send(msg); err != nil {
			wasabee.Log.Error(err)
			return err
		}
		if _, err = bot.DeleteMessage(tgbotapi.NewDeleteMessage(update.CallbackQuery.Message.Chat.ID, update.CallbackQuery.Message.MessageID)); err != nil {
			wasabee.Log.Error(err)
			return err
		}
		return nil
	}

	if update.Message != nil && update.Message.Chat.Type == "private" {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "")
		defaultReply, err := templateExecute("default", update.Message.From.LanguageCode, nil)
		if err != nil {
			wasabee.Log.Error(err)
			return err
		}
		msg.Text = defaultReply
		msg.ParseMode = "MarkDown"

		tgid := wasabee.TelegramID(update.Message.From.ID)
		gid, verified, err := tgid.GidV()
		if err != nil {
			wasabee.Log.Error(err)
			return err
		}

		if gid == "" {
			wasabee.Log.Infow("unknown user; initializing", "subsystem", "Telegram", "tgusername", update.Message.From.UserName, "tgid", tgid)
			fgid, err := runRocks(tgid)
			if fgid != "" && err == nil {
				tmp, _ := templateExecute("InitTwoSuccess", update.Message.From.LanguageCode, nil)
				msg.Text = tmp
			} else {
				err = newUserInit(&msg, &update)
				if err != nil {
					wasabee.Log.Error(err)
				}
			}
			if _, err = bot.Send(msg); err != nil {
				wasabee.Log.Error(err)
				return err
			}
			return nil
		}

		if !verified {
			wasabee.Log.Infof("unverified user; verifying", "subsystem", "Telegram", "tgusername", update.Message.From.UserName, "tgid", tgid)
			err = newUserVerify(&msg, &update)
			if err != nil {
				wasabee.Log.Error(err)
			}
			if _, err = bot.Send(msg); err != nil {
				wasabee.Log.Error(err)
				return err
			}
			return nil
		}

		// verified user, process message
		if err := processMessage(&msg, &update, gid); err != nil {
			wasabee.Log.Error(err)
			return err
		}
	}

	if update.EditedMessage != nil && update.EditedMessage.Location != nil {
		wasabee.Log.Debug("processing live location upate")
		tgid := wasabee.TelegramID(update.EditedMessage.From.ID)
		gid, verified, err := tgid.GidV()
		if err != nil {
			wasabee.Log.Error(err)
			return err
		}
		if !verified || gid == "" {
			wasabee.Log.Debugw("user not initialized/verified, ignoring location update", "GID", gid, "tgid", tgid)
			return nil
		}

		_ = gid.AgentLocation(
			strconv.FormatFloat(update.EditedMessage.Location.Latitude, 'f', -1, 64),
			strconv.FormatFloat(update.EditedMessage.Location.Longitude, 'f', -1, 64),
		)
	}

	return nil
}

func newUserInit(msg *tgbotapi.MessageConfig, inMsg *tgbotapi.Update) error {
	var lockey wasabee.LocKey
	if inMsg.Message.IsCommand() {
		tokens := strings.Split(inMsg.Message.Text, " ")
		if len(tokens) == 2 {
			lockey = wasabee.LocKey(strings.TrimSpace(tokens[1]))
		}
	} else {
		lockey = wasabee.LocKey(strings.TrimSpace(inMsg.Message.Text))
	}

	tid := wasabee.TelegramID(inMsg.Message.From.ID)
	err := tid.InitAgent(inMsg.Message.From.UserName, lockey)
	if err != nil {
		wasabee.Log.Error(err)
		tmp, _ := templateExecute("InitOneFail", inMsg.Message.From.LanguageCode, nil)
		msg.Text = tmp
	} else {
		tmp, _ := templateExecute("InitOneSuccess", inMsg.Message.From.LanguageCode, nil)
		msg.Text = tmp
	}
	return err
}

func newUserVerify(msg *tgbotapi.MessageConfig, inMsg *tgbotapi.Update) error {
	var authtoken string
	if inMsg.Message.IsCommand() {
		tokens := strings.Split(inMsg.Message.Text, " ")
		if len(tokens) == 2 {
			authtoken = tokens[1]
		}
	} else {
		authtoken = inMsg.Message.Text
	}
	authtoken = strings.TrimSpace(authtoken)
	tid := wasabee.TelegramID(inMsg.Message.From.ID)
	err := tid.VerifyAgent(authtoken)
	if err != nil {
		wasabee.Log.Error(err)
		tmp, _ := templateExecute("InitTwoFail", inMsg.Message.From.LanguageCode, nil)
		msg.Text = tmp
	} else {
		tmp, _ := templateExecute("InitTwoSuccess", inMsg.Message.From.LanguageCode, nil)
		msg.Text = tmp
	}
	return err
}

func keyboards(c *TGConfiguration) {
	c.baseKbd = tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButtonLocation("Send Location"),
			tgbotapi.NewKeyboardButton("Teams"),
			tgbotapi.NewKeyboardButton("Teammates Nearby"),
		),
		/* -- disable until can be brought up to current
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("My Assignments"),
			tgbotapi.NewKeyboardButton("Nearby Tasks"),
		),
		*/
	)
}

// This is where command processing takes place
func processMessage(msg *tgbotapi.MessageConfig, inMsg *tgbotapi.Update, gid wasabee.GoogleID) error {
	if inMsg.Message.IsCommand() {
		switch inMsg.Message.Command() {
		case "start":
			tmp, _ := templateExecute("help", inMsg.Message.From.LanguageCode, nil)
			msg.Text = tmp
			msg.ReplyMarkup = config.baseKbd
		case "help":
			tmp, _ := templateExecute("help", inMsg.Message.From.LanguageCode, nil)
			msg.Text = tmp
			msg.ReplyMarkup = config.baseKbd
		default:
			tmp, _ := templateExecute("default", inMsg.Message.From.LanguageCode, nil)
			msg.Text = tmp
			msg.ReplyMarkup = config.baseKbd
		}

		if _, err := bot.DeleteMessage(tgbotapi.NewDeleteMessage(inMsg.Message.Chat.ID, inMsg.Message.MessageID)); err != nil {
			wasabee.Log.Error(err)
			return err
		}
	} else if inMsg.Message.Text != "" {
		switch inMsg.Message.Text {
		/* case "My Assignments":
			msg.ReplyMarkup = assignmentKeyboard(gid)
			msg.Text = "My Assignments"
		case "Nearby Tasks":
			msg.ReplyMarkup = nearbyAssignmentKeyboard(gid)
			msg.Text = "Nearby Tasks" */
		case "Teams":
			msg.ReplyMarkup = teamKeyboard(gid)
			msg.Text = "Your Teams"
		case "Teammates Nearby":
			msg.Text, _ = teammatesNear(gid, inMsg)
			msg.ReplyMarkup = config.baseKbd
			msg.DisableWebPagePreview = true
		default:
			msg.ReplyMarkup = config.baseKbd
		}
	}

	if inMsg.Message != nil && inMsg.Message.Location != nil {
		wasabee.Log.Debugw("processing location", "subsystem", "Telegram", "GID", gid)
		_ = gid.AgentLocation(
			strconv.FormatFloat(inMsg.Message.Location.Latitude, 'f', -1, 64),
			strconv.FormatFloat(inMsg.Message.Location.Longitude, 'f', -1, 64),
		)
	}

	if _, err := bot.Send(msg); err != nil {
		wasabee.Log.Error(err)
		return err
	}

	return nil
}

// SendMessage is registered with Wasabee-Server as a message bus to allow other modules to send messages via Telegram
func SendMessage(gid wasabee.GoogleID, message string) (bool, error) {
	tgid, err := gid.TelegramID()
	if err != nil {
		wasabee.Log.Error(err)
		return false, err
	}
	tgid64 := int64(tgid)
	if tgid64 == 0 {
		wasabee.Log.Debug("TelegramID not found", "subsystem", "Telegram", "GID", gid)
		return false, err
	}
	msg := tgbotapi.NewMessage(tgid64, "")
	msg.Text = message
	msg.ParseMode = "MarkDown"

	_, err = bot.Send(msg)
	if err != nil {
		wasabee.Log.Error(err)
		return false, err
	}

	wasabee.Log.Debugw("sent message", "subsystem", "Telegram", "GID", gid)
	return true, nil
}

func teammatesNear(gid wasabee.GoogleID, inMsg *tgbotapi.Update) (string, error) {
	var td wasabee.TeamData
	var txt = ""
	maxdistance := 500
	maxresults := 10

	err := gid.TeammatesNear(maxdistance, maxresults, &td)
	if err != nil {
		wasabee.Log.Error(err)
		return txt, err
	}
	txt, err = templateExecute("Teammates", inMsg.Message.From.LanguageCode, &td)
	if err != nil {
		wasabee.Log.Error(err)
		return txt, err
	}

	return txt, nil
}

// checks rocks based on tgid, Inits agent if found
// returns gid, tgfound, error
func runRocks(tgid wasabee.TelegramID) (wasabee.GoogleID, error) {
	var agent wasabee.RocksAgent

	err := wasabee.RocksSearch(tgid, &agent)
	if err != nil {
		wasabee.Log.Error(err)
		return "", err
	}
	if agent.Gid == "" {
		return "", nil
	}

	// add to main tables if necessary
	_, err = (agent.Gid).InitAgent()
	if err != nil {
		wasabee.Log.Error(err)
		return agent.Gid, err
	}

	// this adds the agent to the Telegram tables
	// but InitAgent should have already called this ...
	err = wasabee.RocksUpdate(agent.Gid, &agent)
	if err != nil {
		wasabee.Log.Error(err)
		return agent.Gid, err
	}

	return agent.Gid, nil
}
