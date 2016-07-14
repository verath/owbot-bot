package discord

type Gateway struct {
	Url string `json:"url"`
}

type User struct {
	Id            string `json:"id"`
	Username      string `json:"username"`
	Discriminator string `json:"discriminator"`
	Bot           bool   `json:"bot"`
}

type Message struct {
	Id        string `json:"id"`
	ChannelId string `json:"channel_id"`
	Author    *User  `json:"author"`
	Content   string `json:"content"`
	Mentions  []User `json:"mentions"`
}
