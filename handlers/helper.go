package handlers

import "gopkg.in/telebot.v3"

func sendOrEdit(c telebot.Context, msg interface{}, opts ...interface{}) error {
if c.Callback() != nil {
return c.Edit(msg, opts...)
}
return c.Send(msg, opts...)
}
