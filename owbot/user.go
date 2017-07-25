package owbot

import (
	"encoding/json"
	"errors"
	"github.com/Sirupsen/logrus"
	"github.com/boltdb/bolt"
	"io"
)

// A user is a mapping between a Discord user id and
// a battleTag
type User struct {
	// The Discord id (snowflake) of the user
	ID string
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
	io.Closer
	// Returns the user for the provided Discord user id, or nil
	// if no such user exist.
	Get(userID string) (*User, error)

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

func (s *MemoryUserSource) Get(userID string) (*User, error) {
	user, _ := s.data[userID]
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
	s.data[userCopy.ID] = userCopy
	return nil
}

func (s *MemoryUserSource) Close() error {
	return nil
}

var bucketUsers = []byte("users")

type BoltUserSource struct {
	logger *logrus.Entry
	db     *bolt.DB
}

func createUsersBucket(db *bolt.DB) error {
	return db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketUsers)
		return err
	})
}

func NewBoltUserSource(logger *logrus.Logger, db *bolt.DB) (*BoltUserSource, error) {
	// Make sure the users bucket exist
	if err := createUsersBucket(db); err != nil {
		return nil, err
	}

	// Store the logger as an Entry, adding the module to all log calls
	loggerEntry := logger.WithField("module", "boltUserSource")

	return &BoltUserSource{
		db:     db,
		logger: loggerEntry,
	}, nil
}

func (s *BoltUserSource) mustGetBucket(tx *bolt.Tx, name []byte) *bolt.Bucket {
	bucket := tx.Bucket(name)
	if bucket == nil {
		s.logger.WithField("name", name).Panic("Bucket not found")
	}
	return bucket
}

func (s *BoltUserSource) Get(userID string) (*User, error) {
	var user *User
	err := s.db.View(func(tx *bolt.Tx) error {
		bucket := s.mustGetBucket(tx, bucketUsers)
		v := bucket.Get([]byte(userID))
		if v == nil {
			return nil
		}
		user = &User{}
		return json.Unmarshal(v, user)
	})
	return user, err
}

func (s *BoltUserSource) Save(user *User) error {
	if user == nil {
		return errors.New("User can not be nil")
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := s.mustGetBucket(tx, bucketUsers)
		data, err := json.Marshal(user)
		if err != nil {
			return err
		}
		return bucket.Put([]byte(user.ID), data)
	})
}

func (s *BoltUserSource) Close() error {
	return s.db.Close()
}
