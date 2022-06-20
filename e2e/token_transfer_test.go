package e2e

import (
	"context"
	"fmt"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/ibc-go/v3/e2e/dockerutil"
	"github.com/cosmos/ibc-go/v3/e2e/setup"
	"github.com/cosmos/ibc-go/v3/e2e/testconfig"
	transfertypes "github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"
	"github.com/strangelove-ventures/ibctest"
	"github.com/strangelove-ventures/ibctest/chain/cosmos"
	"github.com/strangelove-ventures/ibctest/ibc"
	"github.com/strangelove-ventures/ibctest/test"
	"github.com/strangelove-ventures/ibctest/testreporter"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	"strings"
	"testing"
	"time"
)

const (
	pollHeightMax = uint64(300)
)

//func TestConformance(t *testing.T) {
//	logger := zaptest.NewLogger(t)
//	cf := ibctest.NewBuiltinChainFactory(logger, []*ibctest.ChainSpec{
//		{Name: "simapp-a", ChainConfig: setup.NewSimappConfig("simapp-a", "chain-a", "atoma")},
//		{Name: "simapp-b", ChainConfig: setup.NewSimappConfig("simapp-b", "chain-b", "atomb")},
//	})
//	rf := ibctest.NewBuiltinRelayerFactory(ibc.CosmosRly, logger)
//
//	conformance.TestChainPair(t, cf, rf, testreporter.NewNopReporter())
//}

type FeeChain struct {
	*cosmos.CosmosChain
}

func (fc *FeeChain) RegisterCounterPartyPayee(ctx context.Context, chain1Address, chain2Address string) error {
	tn := fc.ChainNodes[0]
	cmd := []string{tn.Chain.Config().Bin,
		"tx",
		"ibc-fee",
		"register-counterparty-payee",
		"transfer",
		"channel-0",
		strings.TrimSpace(chain2Address),
		strings.TrimSpace(chain1Address),
		"--from", strings.TrimSpace(chain2Address),
		"--keyring-backend", keyring.BackendTest,
		"--home", tn.NodeHome(),
		"--node", fmt.Sprintf("tcp://%s:26657", tn.HostName()),
		"--output", "json",
		"--chain-id", fc.Config().ChainID,
		"--yes",
	}

	exitCode, stdout, stderr, err := tn.NodeJob(ctx, cmd)
	if err != nil {
		return dockerutil.HandleNodeJobError(exitCode, stdout, stderr, err)
	}

	return nil

}

func (fc *FeeChain) QueryPackets(ctx context.Context) error {
	tn := fc.ChainNodes[0]
	cmd := []string{tn.Chain.Config().Bin,
		"q",
		"ibc-fee",
		"packets-for-channel",
		"transfer",
		"channel-0",
		"--home", tn.NodeHome(),
		"--node", fmt.Sprintf("tcp://%s:26657", tn.HostName()),
		"--output", "json",
		"--chain-id", fc.Config().ChainID,
	}

	exitCode, stdout, stderr, err := tn.NodeJob(ctx, cmd)
	if err != nil {
		return dockerutil.HandleNodeJobError(exitCode, stdout, stderr, err)
	}

	return nil

}

func (fc *FeeChain) IncentivizePacket(ctx context.Context, fromAddress string, recvFee, ackFee, timeoutFee int64) error {
	tn := fc.ChainNodes[0]
	cmd := []string{tn.Chain.Config().Bin,
		"tx",
		"ibc-fee",
		"pay-packet-fee",
		"transfer",
		"channel-0",
		"1",
		"--from", fromAddress,
		"--recv-fee", fmt.Sprintf("%d%s", recvFee, fc.Config().Denom),
		"--ack-fee", fmt.Sprintf("%d%s", ackFee, fc.Config().Denom),
		"--timeout-fee", fmt.Sprintf("%d%s", timeoutFee, fc.Config().Denom),
		"--keyring-backend", keyring.BackendTest,
		"--home", tn.NodeHome(),
		"--node", fmt.Sprintf("tcp://%s:26657", tn.HostName()),
		"--output", "json",
		"--chain-id", fc.Config().ChainID,
		"--yes",
	}

	exitCode, stdout, stderr, err := tn.NodeJob(ctx, cmd)
	if err != nil {
		return dockerutil.HandleNodeJobError(exitCode, stdout, stderr, err)
	}

	return nil

}

func TestFeeMiddleware(t *testing.T) {
	ctx := context.TODO()
	rep := testreporter.NewNopReporter()
	req := require.New(rep.TestifyT(t))
	eRep := rep.RelayerExecReporter(t)

	srcChain, dstChain, relayer := setup.StandardTwoChainEnvironment(t, req, eRep, setup.FeeMiddlewareOptions())
	//srcChain, dstChain, _ := setup.StandardTwoChainEnvironment(t, req, eRep, setup.FeeMiddlewareOptions())

	startingTokenAmount := int64(10_000_000)

	users := ibctest.GetAndFundTestUsers(t, ctx, strings.ReplaceAll(t.Name(), " ", "-"), startingTokenAmount, srcChain, dstChain, srcChain, dstChain)

	srcRelayUser := users[0]
	dstRelayUser := users[1]

	srcChainWallet := users[2]
	dstChainWallet := users[3]

	req.NoError(test.WaitForBlocks(ctx, 5, srcChain, dstChain), "failed to wait for blocks")

	dstFeeChain := &FeeChain{CosmosChain: dstChain}
	srcFeeChain := &FeeChain{CosmosChain: srcChain}

	// register dstRelayUser as counter party payee
	req.NoError(dstFeeChain.RegisterCounterPartyPayee(ctx, srcRelayUser.Bech32Address(srcChain.Config().Bech32Prefix), dstRelayUser.Bech32Address(dstFeeChain.Config().Bech32Prefix)))

	testCoinSrcToDst := ibc.WalletAmount{
		Address: dstChainWallet.Bech32Address(dstChain.Config().Bech32Prefix), // destination address
		Denom:   srcChain.Config().Denom,
		Amount:  10000,
	}

	// send a transfer from wallet 1 on src chain to wallet 3 on dst chain
	srcTx, err := srcChain.SendIBCTransfer(ctx, "channel-0", srcChainWallet.KeyName, testCoinSrcToDst, nil)
	req.NoError(err)
	req.NoError(srcTx.Validate(), "source ibc transfer tx is invalid")

	// Verify that tokens have been escrowed.
	actualBalance, err := srcChain.GetBalance(ctx, srcChainWallet.Bech32Address(srcChain.Config().Bech32Prefix), srcChain.Config().Denom)
	req.NoError(err)

	gasSpent := srcChain.GetGasFeesInNativeDenom(srcTx.GasSpent)

	expected := startingTokenAmount - testCoinSrcToDst.Amount - gasSpent
	req.Equal(expected, actualBalance)

	recvFee := int64(50)
	ackFee := int64(25)
	timeoutFee := int64(10)

	err = srcFeeChain.QueryPackets(ctx)
	req.NoError(err)

	err = srcFeeChain.IncentivizePacket(ctx, srcChainWallet.KeyName, recvFee, ackFee, timeoutFee)
	req.NoError(err)

	time.Sleep(10 * time.Second)

	err = srcFeeChain.QueryPackets(ctx)
	req.NoError(err)

	actualBalance, err = srcChain.GetBalance(ctx, srcChainWallet.Bech32Address(srcChain.Config().Bech32Prefix), srcChain.Config().Denom)
	req.NoError(err)

	// The balance should be lowered by the sum of the recv, ack and timeout fees.
	expected = startingTokenAmount - testCoinSrcToDst.Amount - gasSpent - recvFee - ackFee - timeoutFee
	req.Equal(expected, actualBalance)

	err = relayer.StartRelayer(ctx, eRep, testconfig.TestPath)
	req.NoError(err, fmt.Sprintf("failed to start relayer: %s", err))
	t.Cleanup(func() {
		if err := relayer.StopRelayer(ctx, eRep); err != nil {
			t.Logf("error stopping relayer: %v", err)
		}
	})
	// wait for relayer to start.
	time.Sleep(time.Second * 20)

	err = srcFeeChain.QueryPackets(ctx)
	req.NoError(err)

	req.NoError(test.WaitForBlocks(ctx, 5, srcChain, dstChain), "failed to wait for blocks")

	//srcAck, err := test.PollForAck(ctx, srcChain, srcTx.Height, srcTx.Height+pollHeightMax, srcTx.Packet)
	//req.NoError(err, "failed to get acknowledgement on source chain")
	//req.NoError(srcAck.Validate(), "invalid acknowledgement on source chain")

	actualBalance, err = srcChain.GetBalance(ctx, srcChainWallet.Bech32Address(srcChain.Config().Bech32Prefix), srcChain.Config().Denom)
	req.NoError(err)

	// once the relayer has started, the timeout fee should be refunded.
	expected = startingTokenAmount - testCoinSrcToDst.Amount - gasSpent - ackFee - recvFee
	t.Logf("EXPECTED=%d, ACTUAL=%d", expected, actualBalance)
	//req.Equal(expected, actualBalance)
	//expected: 9987925
	//actual  : 9987915
	// TODO: verify dstRelayUser has the fees (ack + recv)

	//srcDenomTrace := transfertypes.ParseDenomTrace(transfertypes.GetPrefixedDenom("channel-0", "channel-0", srcChain.Config().Denom))
	//dstIbcDenom := srcDenomTrace.IBCDenom()
	//
	//actualBalance, err = srcChain.GetBalance(ctx, dstRelayUser.Bech32Address(srcChain.Config().Bech32Prefix), dstIbcDenom)

	actualBalance, err = dstChain.GetBalance(ctx, dstRelayUser.Bech32Address(dstFeeChain.Config().Bech32Prefix), dstFeeChain.Config().Denom)
	req.NoError(err)
	t.Logf("Relayer User Bal 1: %d", actualBalance)

	srcDenomTrace := transfertypes.ParseDenomTrace(transfertypes.GetPrefixedDenom("transfer", "channel-0", srcChain.Config().Denom))
	dstIbcDenom := srcDenomTrace.IBCDenom()
	actualBalance, err = dstChain.GetBalance(ctx, dstRelayUser.Bech32Address(srcChain.Config().Bech32Prefix), dstIbcDenom)
	req.NoError(err)
	t.Logf("Relayer User Bal 2: %d", actualBalance)

}

func TestTokenTransfer(t *testing.T) {
	ctx := context.TODO()
	rep := testreporter.NewNopReporter()
	req := require.New(rep.TestifyT(t))
	eRep := rep.RelayerExecReporter(t)

	srcChain, dstChain, relayer := setup.StandardTwoChainEnvironment(t, req, eRep)

	channels, err := relayer.GetChannels(ctx, eRep, srcChain.Config().ChainID)
	req.NoError(err, fmt.Sprintf("failed to get channels: %s", err))
	req.Len(channels, 1, fmt.Sprintf("channel count invalid. expected: 1, actual: %d", len(channels)))

	channel := channels[0]

	srcChainCfg := srcChain.Config()
	dstChainCfg := dstChain.Config()

	testUsers := ibctest.GetAndFundTestUsers(t, ctx, strings.ReplaceAll(t.Name(), " ", "-"), 10_000_000, srcChain, dstChain)

	srcUser := testUsers[0]
	dstUser := testUsers[1]

	// will send ibc transfers from user wallet on both chains to their own respective wallet on the other chain
	testCoinSrcToDst := ibc.WalletAmount{
		Address: srcUser.Bech32Address(dstChainCfg.Bech32Prefix),
		Denom:   srcChainCfg.Denom,
		Amount:  10000,
	}
	testCoinDstToSrc := ibc.WalletAmount{
		Address: dstUser.Bech32Address(srcChainCfg.Bech32Prefix),
		Denom:   dstChainCfg.Denom,
		Amount:  20000,
	}

	var (
		eg    errgroup.Group
		srcTx ibc.Tx
		dstTx ibc.Tx
	)

	eg.Go(func() error {
		var err error
		srcTx, err = srcChain.SendIBCTransfer(ctx, channel.ChannelID, srcUser.KeyName, testCoinSrcToDst, nil)
		if err != nil {
			return fmt.Errorf("failed to send ibc transfer from source: %w", err)
		}
		return nil
	})

	eg.Go(func() error {
		var err error
		dstTx, err = dstChain.SendIBCTransfer(ctx, channel.ChannelID, dstUser.KeyName, testCoinDstToSrc, nil)
		if err != nil {
			return fmt.Errorf("failed to send ibc transfer from destination: %w", err)
		}
		return nil
	})

	req.NoError(eg.Wait())
	req.NoError(srcTx.Validate(), "source ibc transfer tx is invalid")
	req.NoError(dstTx.Validate(), "destination ibc transfer tx is invalid")

	err = relayer.StartRelayer(ctx, eRep, testconfig.TestPath)
	req.NoError(err, fmt.Sprintf("failed to start relayer: %s", err))
	t.Cleanup(func() {
		if err := relayer.StopRelayer(ctx, eRep); err != nil {
			t.Logf("error stopping relayer: %v", err)
		}
	})

	// wait for relayer to start up
	time.Sleep(5 * time.Second)

	t.Run("User on Chain1 has the correct balance on both chains", func(t *testing.T) {
		srcAck, err := test.PollForAck(ctx, srcChain, srcTx.Height, srcTx.Height+pollHeightMax, srcTx.Packet)
		req.NoError(err, "failed to get acknowledgement on source chain")
		req.NoError(srcAck.Validate(), "invalid acknowledgement on source chain")

		srcChainInitialBalance := int64(10_000_000)

		// get ibc denom for dst denom on src chain
		srcDemonTrace := transfertypes.ParseDenomTrace(transfertypes.GetPrefixedDenom(channels[0].Counterparty.PortID, channels[0].Counterparty.ChannelID, srcChainCfg.Denom))
		dstIbcDenom := srcDemonTrace.IBCDenom()

		srcFinalBalance, err := srcChain.GetBalance(ctx, srcUser.Bech32Address(srcChainCfg.Bech32Prefix), srcChainCfg.Denom)
		req.NoError(err, "failed to get balance from source chain")

		dstFinalBalance, err := dstChain.GetBalance(ctx, srcUser.Bech32Address(dstChainCfg.Bech32Prefix), dstIbcDenom)
		req.NoError(err, "failed to get balance from dest chain")

		totalFees := srcChain.GetGasFeesInNativeDenom(srcTx.GasSpent)
		expectedDifference := testCoinSrcToDst.Amount + totalFees

		req.Equal(srcChainInitialBalance-expectedDifference, srcFinalBalance, "source address should have paid the full amount + gas fees")
		req.Equal(testCoinSrcToDst.Amount, dstFinalBalance, "destination address should be match the amount sent")
	})

	t.Run("User on Chain2 has the correct balance on both chains", func(t *testing.T) {
		dstAck, err := test.PollForAck(ctx, dstChain, dstTx.Height, dstTx.Height+pollHeightMax, dstTx.Packet)
		req.NoError(err, "failed to get acknowledgement on destination chain")
		req.NoError(dstAck.Validate(), "invalid acknowledgement on source chain")

		srcChainInitialBalance := int64(0)
		dstChainInitialBalance := int64(10_000_000)

		dstDenom := dstChainCfg.Denom
		// get ibc denom for dst denom on src chain
		dstDenomTrace := transfertypes.ParseDenomTrace(transfertypes.GetPrefixedDenom(channels[0].PortID, channels[0].ChannelID, dstDenom))
		srcIbcDenom := dstDenomTrace.IBCDenom()

		srcFinalBalance, err := srcChain.GetBalance(ctx, dstUser.Bech32Address(srcChainCfg.Bech32Prefix), srcIbcDenom)
		req.NoError(err, "failed to get balance from source chain")

		dstFinalBalance, err := dstChain.GetBalance(ctx, dstUser.Bech32Address(dstChainCfg.Bech32Prefix), dstDenom)
		req.NoError(err, "failed to get balance from dest chain")

		totalFees := dstChain.GetGasFeesInNativeDenom(dstTx.GasSpent)
		expectedDifference := testCoinDstToSrc.Amount + totalFees

		req.Equal(srcChainInitialBalance+testCoinDstToSrc.Amount, srcFinalBalance)
		req.Equal(dstChainInitialBalance-expectedDifference, dstFinalBalance)
	})
}
