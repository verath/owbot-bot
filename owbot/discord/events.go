package discord

type EventReady struct {
}

type EventMessageCreate struct {
	*Message
}
