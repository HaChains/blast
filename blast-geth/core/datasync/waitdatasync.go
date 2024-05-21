package datasync

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/log"
	"github.com/go-redis/redis"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func mustAtoi(s string) int {
	val, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return val
}

var (
	offsetBlock    = -1
	newestRedisKey = ``
	chainName      = ``
)

var (
	once       = &sync.Once{}
	global     *redis.Client
	syncBlock  = int64(0)
	signalExit = false
	ChanSignal = make(chan os.Signal, 1)
)

func init() {
	offsetBlock = mustAtoi(os.Getenv("HA_DATA_SYNC_WAIT_OFFSET_BLOCK"))
	if offsetBlock == 0 {
		offsetBlock = 128
	}
	newestRedisKey = os.Getenv("HA_DATA_SYNC_WAIT_NEWEST_REDIS_KEY")
	if newestRedisKey == "" {
		log.Error("HA_DATA_SYNC_WAIT_NEWEST_REDIS_KEY is required")
		os.Exit(-1)
	}
	chainName = os.Getenv("HA_DATA_SYNC_WAIT_CHAIN_NAME")
	if chainName == "" {
		log.Error("HA_DATA_SYNC_WAIT_CHAIN_NAME is required")
		os.Exit(-1)
	}
}

func instance() *redis.Client {
	if global == nil {
		once.Do(func() {
			for {
				if global == nil {
					global = redis.NewFailoverClient(&redis.FailoverOptions{
						MasterName:    os.Getenv("HA_DATA_SYNC_WAIT_REDIS_MASTER_NAME"),
						SentinelAddrs: strings.Split(os.Getenv("HA_DATA_SYNC_WAIT_REDIS_SENTINEL_ADDRS"), ","),
						Password:      os.Getenv("HA_DATA_SYNC_WAIT_REDIS_PASSWORD"),
						DB:            mustAtoi(os.Getenv("HA_DATA_SYNC_WAIT_REDIS_DB")),
					})
				}
				pong, err := global.Ping().Result()
				if err == nil && strings.Compare(pong, `PONG`) == 0 {
					log.Info("successfully connected to redis!")
					break
				} else {
					log.Error("failed to connect redis!")
					global = nil
					time.Sleep(time.Second * 3)
				}
			}
		})
		signal.Notify(ChanSignal, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGKILL, syscall.SIGSTOP)
		go func() {
			sig := <-ChanSignal
			signalExit = true
			log.Info("signal received: " + sig.String())
		}()
	}
	return global
}

func loadSyncNewestBlock() (int64, error) {

	result, err := instance().HGet(newestRedisKey, chainName).Result()
	if err != nil && err.Error() == `redis: nil` {
		return 0, errors.New(fmt.Sprintf("not found key: %s", newestRedisKey))
	} else if err != nil {
		return 0, err
	}
	type BlockTime struct {
		BlockNumber int64 `json:"blockNumber"`
		Timestamp   int64 `json:"timestamp"`
	}
	var block BlockTime
	err = json.Unmarshal([]byte(result), &block)
	if err != nil {
		return 0, err
	}
	return block.BlockNumber, nil
}

func WaitDataSync(currentBlock int64) {

	if currentBlock < syncBlock+int64(offsetBlock) || signalExit {
		return
	}
	for {
		block, err := loadSyncNewestBlock()
		if err != nil {
			log.Error(fmt.Sprintf("latest synced block number：%s unable to load：%s", newestRedisKey, err.Error()))
			time.Sleep(3 * time.Second)
		} else {
			syncBlock = block
			if currentBlock < syncBlock+int64(offsetBlock) {
				return
			} else {
				if rand.Int()%100 < 1 {
					log.Info(fmt.Sprintf("watting for data sync：currentBlock = %d， finishedSyncBlock = %d", currentBlock, syncBlock))
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		if signalExit {
			return
		}
	}
}
