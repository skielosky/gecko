// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/encdb"
	"github.com/ava-labs/gecko/database/prefixdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/vms/components/codec"

	jsoncodec "github.com/ava-labs/gecko/utils/json"
)

var (
	errEmptyUsername = errors.New("username can't be the empty string")
)

// KeyValuePair ...
type KeyValuePair struct {
	Key   []byte `serialize:"true"`
	Value []byte `serialize:"true"`
}

// UserDB describes the full content of a user
type UserDB struct {
	User `serialize:"true"`
	Data []KeyValuePair `serialize:"true"`
}

// Keystore is the RPC interface for keystore management
type Keystore struct {
	lock sync.Mutex
	log  logging.Logger

	codec codec.Codec

	// Key: username
	// Value: The user with that name
	users map[string]*User

	// Used to persist users and their data
	userDB database.Database
	bcDB   database.Database
	//           BaseDB
	//          /      \
	//    UserDB        BlockchainDB
	//                 /      |     \
	//               Usr     Usr    Usr
	//            /   |   \
	//          BID  BID  BID
}

// Initialize the keystore
func (ks *Keystore) Initialize(log logging.Logger, db database.Database) {
	ks.log = log
	ks.codec = codec.NewDefault()
	ks.users = make(map[string]*User)
	ks.userDB = prefixdb.New([]byte("users"), db)
	ks.bcDB = prefixdb.New([]byte("bcs"), db)
}

// CreateHandler returns a new service object that can send requests to thisAPI.
func (ks *Keystore) CreateHandler() *common.HTTPHandler {
	newServer := rpc.NewServer()
	codec := jsoncodec.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	newServer.RegisterService(ks, "keystore")
	return &common.HTTPHandler{LockOptions: common.NoLock, Handler: newServer}
}

// Get the user whose name is [username]
func (ks *Keystore) getUser(username string) (*User, error) {
	// If the user is already in memory, return it
	usr, exists := ks.users[username]
	if exists {
		return usr, nil
	}
	// The user is not in memory; try the database
	usrBytes, err := ks.userDB.Get([]byte(username))
	if err != nil { // Most likely bc user doesn't exist in database
		return nil, err
	}

	usr = &User{}
	return usr, ks.codec.Unmarshal(usrBytes, usr)
}

// CreateUserArgs are arguments for passing into CreateUser requests
type CreateUserArgs struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// CreateUserReply is the response from calling CreateUser
type CreateUserReply struct {
	Success bool `json:"success"`
}

// CreateUser creates an empty user with the provided username and password
func (ks *Keystore) CreateUser(_ *http.Request, args *CreateUserArgs, reply *CreateUserReply) error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	ks.log.Verbo("CreateUser called with %s", args.Username)

	if args.Username == "" {
		return errEmptyUsername
	}
	if usr, err := ks.getUser(args.Username); err == nil || usr != nil {
		return fmt.Errorf("user already exists: %s", args.Username)
	}

	usr := &User{}
	if err := usr.Initialize(args.Password); err != nil {
		return err
	}

	usrBytes, err := ks.codec.Marshal(usr)
	if err != nil {
		return err
	}

	if err := ks.userDB.Put([]byte(args.Username), usrBytes); err != nil {
		return err
	}
	ks.users[args.Username] = usr
	reply.Success = true
	return nil
}

// ListUsersArgs are the arguments to ListUsers
type ListUsersArgs struct{}

// ListUsersReply is the reply from ListUsers
type ListUsersReply struct {
	Users []string `json:"users"`
}

// ListUsers lists all the registered usernames
func (ks *Keystore) ListUsers(_ *http.Request, args *ListUsersArgs, reply *ListUsersReply) error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	ks.log.Verbo("ListUsers called")

	reply.Users = []string{}

	it := ks.userDB.NewIterator()
	defer it.Release()
	for it.Next() {
		reply.Users = append(reply.Users, string(it.Key()))
	}
	return it.Error()
}

// ExportUserArgs are the arguments to ExportUser
type ExportUserArgs struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// ExportUserReply is the reply from ExportUser
type ExportUserReply struct {
	User string `json:"user"`
}

// ExportUser exports a serialized encoding of a user's information complete with encrypted database values
func (ks *Keystore) ExportUser(_ *http.Request, args *ExportUserArgs, reply *ExportUserReply) error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	ks.log.Verbo("ExportUser called for %s", args.Username)

	usr, err := ks.getUser(args.Username)
	if err != nil {
		return err
	}
	if !usr.CheckPassword(args.Password) {
		return fmt.Errorf("incorrect password for %s", args.Username)
	}

	userDB := prefixdb.New([]byte(args.Username), ks.bcDB)

	userData := UserDB{
		User: *usr,
	}

	it := userDB.NewIterator()
	defer it.Release()
	for it.Next() {
		userData.Data = append(userData.Data, KeyValuePair{
			Key:   it.Key(),
			Value: it.Value(),
		})
	}
	if err := it.Error(); err != nil {
		return err
	}

	b, err := ks.codec.Marshal(&userData)
	if err != nil {
		return err
	}
	cb58 := formatting.CB58{Bytes: b}
	reply.User = cb58.String()
	return nil
}

// ImportUserArgs are arguments for ImportUser
type ImportUserArgs struct {
	Username string `json:"username"`
	Password string `json:"password"`
	User     string `json:"user"`
}

// ImportUserReply is the response for ImportUser
type ImportUserReply struct {
	Success bool `json:"success"`
}

// ImportUser imports a serialized encoding of a user's information complete with encrypted database values, integrity checks the password, and adds it to the database
func (ks *Keystore) ImportUser(r *http.Request, args *ImportUserArgs, reply *ImportUserReply) error {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	ks.log.Verbo("ImportUser called for %s", args.Username)

	if usr, err := ks.getUser(args.Username); err == nil || usr != nil {
		return fmt.Errorf("user already exists: %s", args.Username)
	}

	cb58 := formatting.CB58{}
	if err := cb58.FromString(args.User); err != nil {
		return err
	}

	userData := UserDB{}
	if err := ks.codec.Unmarshal(cb58.Bytes, &userData); err != nil {
		return err
	}

	usrBytes, err := ks.codec.Marshal(&userData.User)
	if err != nil {
		return err
	}

	// TODO: Should add batching to prevent creating a user without importing
	// the account
	if err := ks.userDB.Put([]byte(args.Username), usrBytes); err != nil {
		return err
	}
	ks.users[args.Username] = &userData.User

	userDB := prefixdb.New([]byte(args.Username), ks.bcDB)
	batch := userDB.NewBatch()

	for _, kvp := range userData.Data {
		batch.Put(kvp.Key, kvp.Value)
	}

	reply.Success = true
	return batch.Write()
}

// NewBlockchainKeyStore ...
func (ks *Keystore) NewBlockchainKeyStore(blockchainID ids.ID) *BlockchainKeystore {
	return &BlockchainKeystore{
		blockchainID: blockchainID,
		ks:           ks,
	}
}

// GetDatabase ...
func (ks *Keystore) GetDatabase(bID ids.ID, username, password string) (database.Database, error) {
	ks.lock.Lock()
	defer ks.lock.Unlock()

	usr, err := ks.getUser(username)
	if err != nil {
		return nil, err
	}
	if !usr.CheckPassword(password) {
		return nil, fmt.Errorf("incorrect password for user '%s'", username)
	}

	userDB := prefixdb.New([]byte(username), ks.bcDB)
	bcDB := prefixdb.NewNested(bID.Bytes(), userDB)
	encDB, err := encdb.New([]byte(password), bcDB)

	if err != nil {
		return nil, err
	}

	return encDB, nil
}
