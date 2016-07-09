package owbot

// A user is a mapping between a Discord user id and
// a battleTag
type User struct {
	// The Discord id (snowflake) of the user
	Id string
	// The Battle.net BattleTag for the user
	BattleTag string
	// The Discord id (snowflake) of the user that last
	// created or updated this User entry. Used so we can
	// prioritize the "real" user, while still letting others
	// set a BattleTag if the user has not set one.
	CreatedBy string
}

// A simple interface for a data source of users
type UserSource interface {
	// Returns the user for the provided Discord user id, or nil
	// if no such user exist.
	Get(userId string) (*User, error)

	// Stores a user to the data source
	Save(user *User) error
}

// An in memory implementation of a user source
type MemoryUserSource struct {
	data map[string]*User
}

func NewMemoryUserSource() *MemoryUserSource {
	return &MemoryUserSource{
		data: make(map[string]*User),
	}
}

func (s *MemoryUserSource) Get(userId string) (*User, error) {
	user, _ := s.data[userId]
	if user == nil {
		return user, nil
	} else {
		userCopy := new(User)
		*userCopy = *user
		return userCopy, nil
	}
}

func (s *MemoryUserSource) Save(user *User) error {
	userCopy := new(User)
	*userCopy = *user
	s.data[userCopy.Id] = userCopy
	return nil
}
