package service

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
)

// ChainCommitter 区块链种子承诺提交器
type ChainCommitter struct {
	rpcURL     string
	privateKey string
	chainID    int64
	log        *zap.Logger
}

// NewChainCommitter 创建区块链种子承诺提交器
// 若 rpcURL 为空，CommitHash 会优雅降级为本地模式（返回空字符串）
func NewChainCommitter(rpcURL, privateKey string, chainID int64, log *zap.Logger) *ChainCommitter {
	return &ChainCommitter{
		rpcURL:     strings.TrimSpace(rpcURL),
		privateKey: strings.TrimSpace(privateKey),
		chainID:    chainID,
		log:        log,
	}
}

// CommitHash 将 seedHash 作为交易 data 发送到链上（零值自转账）
// 返回交易哈希和网络名称。当 rpcURL 为空时返回空字符串（本地模式）。
func (c *ChainCommitter) CommitHash(ctx context.Context, seedHash []byte) (txHash string, network string, err error) {
	if c.rpcURL == "" {
		c.log.Debug("链上提交已跳过：未配置 RPC URL")
		return "", "", nil
	}
	if c.privateKey == "" {
		c.log.Debug("链上提交已跳过：未配置私钥")
		return "", "", nil
	}

	client, err := ethclient.DialContext(ctx, c.rpcURL)
	if err != nil {
		return "", "", fmt.Errorf("连接以太坊节点失败: %w", err)
	}
	defer client.Close()

	privKey, err := crypto.HexToECDSA(strings.TrimPrefix(c.privateKey, "0x"))
	if err != nil {
		return "", "", fmt.Errorf("解析私钥失败: %w", err)
	}

	publicKey := privKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return "", "", fmt.Errorf("无法获取公钥")
	}
	fromAddr := crypto.PubkeyToAddress(*publicKeyECDSA)

	nonce, err := client.PendingNonceAt(ctx, fromAddr)
	if err != nil {
		return "", "", fmt.Errorf("获取 nonce 失败: %w", err)
	}

	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return "", "", fmt.Errorf("获取 gas price 失败: %w", err)
	}

	chainID := big.NewInt(c.chainID)
	if c.chainID == 0 {
		chainID, err = client.ChainID(ctx)
		if err != nil {
			return "", "", fmt.Errorf("获取 chain ID 失败: %w", err)
		}
	}

	// 自转账，value=0，data=seedHash
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: gasPrice,
		GasFeeCap: new(big.Int).Mul(gasPrice, big.NewInt(2)),
		Gas:       uint64(30000 + len(seedHash)*68),
		To:        &fromAddr,
		Value:     big.NewInt(0),
		Data:      seedHash,
	})

	signer := types.NewLondonSigner(chainID)
	signedTx, err := types.SignTx(tx, signer, privKey)
	if err != nil {
		return "", "", fmt.Errorf("签名交易失败: %w", err)
	}

	if err := client.SendTransaction(ctx, signedTx); err != nil {
		return "", "", fmt.Errorf("发送交易失败: %w", err)
	}

	hash := signedTx.Hash()
	networkName := fmt.Sprintf("evm:%d", chainID.Int64())

	c.log.Info("种子哈希已提交到链上",
		zap.String("txHash", hash.Hex()),
		zap.String("network", networkName),
		zap.String("from", fromAddr.Hex()),
	)

	return hash.Hex(), networkName, nil
}

// Enabled 返回是否已配置链上提交
func (c *ChainCommitter) Enabled() bool {
	return c.rpcURL != "" && c.privateKey != ""
}

// compile-time check: common.Address usage
var _ = common.Address{}
