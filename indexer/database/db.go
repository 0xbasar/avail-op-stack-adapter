// Database module defines the data DB struct which wraps specific DB interfaces for L1/L2 block headers, contract events, bridging schemas.
package database

import (
	"context"
	"fmt"
	"os"

	"github.com/ethereum-optimism/optimism/indexer/config"
	_ "github.com/ethereum-optimism/optimism/indexer/database/serializers"
	"github.com/ethereum-optimism/optimism/op-service/retry"
	"github.com/pkg/errors"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type DB struct {
	gorm *gorm.DB

	Blocks             BlocksDB
	ContractEvents     ContractEventsDB
	BridgeTransfers    BridgeTransfersDB
	BridgeMessages     BridgeMessagesDB
	BridgeTransactions BridgeTransactionsDB
}

func NewDB(dbConfig config.DBConfig) (*DB, error) {
	var db *DB

	retryStrategy := &retry.ExponentialStrategy{Min: 1000, Max: 20_000, MaxJitter: 250}

	_, err := retry.Do[interface{}](context.Background(), 10, retryStrategy, func() (interface{}, error) {
		dsn := fmt.Sprintf("host=%s port=%d dbname=%s sslmode=disable", dbConfig.Host, dbConfig.Port, dbConfig.Name)
		if dbConfig.User != "" {
			dsn += fmt.Sprintf(" user=%s", dbConfig.User)
		}
		if dbConfig.Password != "" {
			dsn += fmt.Sprintf(" password=%s", dbConfig.Password)
		}
		gorm, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
			SkipDefaultTransaction: true,
			Logger:                 logger.Default.LogMode(logger.Silent),
		})

		if err != nil {
			return nil, errors.Wrap(err, "failed to connect to database")
		}

		db = &DB{
			gorm:               gorm,
			Blocks:             newBlocksDB(gorm),
			ContractEvents:     newContractEventsDB(gorm),
			BridgeTransfers:    newBridgeTransfersDB(gorm),
			BridgeMessages:     newBridgeMessagesDB(gorm),
			BridgeTransactions: newBridgeTransactionsDB(gorm),
		}
		return nil, nil
	})

	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to database after multiple retries")
	}

	if err := db.executeSQLMigration(); err != nil {
		return nil, errors.Wrap(err, "failed to execute SQL migration")
	}

	return db, nil
}

// Transaction executes all operations conducted with the supplied database in a single
// transaction. If the supplied function errors, the transaction is rolled back.
func (db *DB) Transaction(fn func(db *DB) error) error {
	return db.gorm.Transaction(func(tx *gorm.DB) error {
		return fn(dbFromGormTx(tx))
	})
}

func (db *DB) Close() error {
	sql, err := db.gorm.DB()
	if err != nil {
		return err
	}

	return sql.Close()
}

func dbFromGormTx(tx *gorm.DB) *DB {
	return &DB{
		gorm:               tx,
		Blocks:             newBlocksDB(tx),
		ContractEvents:     newContractEventsDB(tx),
		BridgeTransfers:    newBridgeTransfersDB(tx),
		BridgeMessages:     newBridgeMessagesDB(tx),
		BridgeTransactions: newBridgeTransactionsDB(tx),
	}
}

func (db *DB) executeSQLMigration() error {
	file, err := os.ReadFile("migrations/20230523_create_schema.sql")
	if err != nil {
		return errors.Wrap(err, "Error reading SQL file")
	}
	if err := db.gorm.Exec(string(file)).Error; err != nil {
		return errors.Wrap(err, "Error executing SQL script")
	}
	return nil
}
