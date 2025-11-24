package menu

import "gopkg.in/telebot.v3"


var MenuOrganizer = &telebot.ReplyMarkup{
    ReplyKeyboard: [][]telebot.ReplyButton{
        {
            {Text: "Создать тендер"},
            {Text: "Мои тендеры"},
        },
		{
            {Text: "История"},
            {Text: "Удалить тендер"},
        },
    },
    ResizeKeyboard: true,
}

var MenuSupplierRegistered = &telebot.ReplyMarkup {
	ReplyKeyboard: [][]telebot.ReplyButton{
        {
            {Text: "Тендеры"},
            {Text: "Подать заявку"},
		},
    },
    ResizeKeyboard: true,
    OneTimeKeyboard: false,
}

var MenuOrganizerCancel = &telebot.ReplyMarkup{
    ReplyKeyboard: [][]telebot.ReplyButton{
        {
            {Text: "Отмена"},
        },
    },
    ResizeKeyboard: true,
}

var MenuSupplierUnregistered = &telebot.ReplyMarkup {
	ReplyKeyboard: [][]telebot.ReplyButton{
        {
            {Text: "Регистрация"},
		},
    },
    ResizeKeyboard: true,
}



var MenuAdmin = &telebot.ReplyMarkup {
	ReplyKeyboard: [][]telebot.ReplyButton{
        {
            {Text: "Пользователи"},
            {Text: "Заявки на регистрацию"},
            
		},
        {
            {Text: "История"},
        },
    },
    ResizeKeyboard: true,
    OneTimeKeyboard: false,
}

var BtnDeleteTender = telebot.InlineButton{
    Unique: "delete_tender",
    Text:   "🗑️ Удалить тендер",
}

var BtnJoinTender = telebot.InlineButton{
	Unique:  "join_tender",
	Text: "📝 Участвовать в тендере",
}

var BtnLeaveTender = telebot.InlineButton{
	Unique:  "leave_tender",
	Text: "🚫 Отменить участие в тендере",
}

