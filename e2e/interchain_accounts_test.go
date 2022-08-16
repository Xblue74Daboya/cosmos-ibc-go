package e2e

import (
	"context"
	"testing"

	ibctest "github.com/strangelove-ventures/ibctest"
	"github.com/strangelove-ventures/ibctest/chain/cosmos"
	"github.com/strangelove-ventures/ibctest/ibc"
	"github.com/stretchr/testify/suite"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	intertxtypes "github.com/cosmos/interchain-accounts/x/inter-tx/types"

	"github.com/cosmos/ibc-go/e2e/testconfig"
	"github.com/cosmos/ibc-go/e2e/testsuite"
	"github.com/cosmos/ibc-go/e2e/testvalues"
)

func TestInterchainAccountsTestSuite(t *testing.T) {
	testconfig.SetChainBinaryVersions(
		"ghcr.io/cosmos/ibc-go-icad", "master", "icad", "ghcr.io/cosmos/ibc-go-icad", "master", "icad",
	)
	suite.Run(t, new(InterchainAccountsTestSuite))
}

type InterchainAccountsTestSuite struct {
	testsuite.E2ETestSuite
}

// RegisterICA will attempt to register an interchain account on the counterparty chain.
func (s *InterchainAccountsTestSuite) RegisterICA(ctx context.Context, chain *cosmos.CosmosChain, user *ibctest.User, fromAddress, connectionID string) error {
	version := "" // allow app to handle the version as appropriate.
	msg := intertxtypes.NewMsgRegisterAccount(fromAddress, connectionID, version)
	txResp, err := s.BroadcastMessages(ctx, chain, user, msg)
	s.AssertValidTxResponse(txResp)
	return err
}

func (s *InterchainAccountsTestSuite) TestInterchainAccounts() {
	t := s.T()
	ctx := context.TODO()

	// setup relayers and connection-0 between two chains
	// channel-0 is a transfer channel but it will not be used in this test case
	relayer, channelA := s.SetupChainsRelayerAndChannel(ctx)
	chainA, chainB := s.GetChains()

	_ = channelA
	connectionId := "connection-0"

	// setup 2 accounts: controller account on chain A, a second chain B account.
	// host account will be created when the ICA is registered
	controllerAccount := s.CreateUserOnChainA(ctx, testvalues.StartingTokenAmount)
	chainBAccount := s.CreateUserOnChainB(ctx, testvalues.StartingTokenAmount)
	var hostAccount string

	t.Run("success: register interchain account", func(t *testing.T) {
		err := s.RegisterICA(ctx, chainA, controllerAccount, controllerAccount.Bech32Address(chainA.Config().Bech32Prefix), connectionId)
		s.Require().NoError(err)
	})

	t.Run("start relayer", func(t *testing.T) {
		s.StartRelayer(relayer)
	})

	t.Run("success: verify interchain account", func(t *testing.T) {
		var err error
		hostAccount, err = s.QueryInterchainAccount(ctx, chainA, controllerAccount.Bech32Address(chainA.Config().Bech32Prefix), connectionId)
		s.Require().NoError(err)
		s.Require().NotZero(len(hostAccount))

		channels, err := relayer.GetChannels(ctx, s.GetRelayerExecReporter(), chainA.Config().ChainID)
		s.Require().NoError(err)
		s.Require().Equal(len(channels), 2)

	})

	t.Run("fail: execute bank transfer over ICA, host account has no funds ", func(t *testing.T) {

		hostAccountBalance, err := chainB.GetBalance(ctx, hostAccount, chainB.Config().Denom)
		s.Require().NoError(err)
		s.Require().Zero(hostAccountBalance)

		// assemble bank transfer message from host account to user account on host chain
		transferMsg := &banktypes.MsgSend{
			FromAddress: hostAccount,
			ToAddress:   chainBAccount.Bech32Address(chainB.Config().Bech32Prefix),
			Amount:      sdk.NewCoins(testvalues.DefaultTransferAmount(chainB.Config().Denom)),
		}

		// assemble submitMessage tx for intertx
		submitMsg, err := intertxtypes.NewMsgSubmitTx(
			transferMsg,
			connectionId,
			controllerAccount.Bech32Address(chainA.Config().Bech32Prefix),
		)
		s.Require().NoError(err)

		// broadcast submitMessage tx from controller account on chain A
		// this message should trigger the sending of an ICA packet over channel-1 (channel created between controller and host)
		// this ICA packet contains the assembled bank transfer message from above, which will be executed by the host account on the host chain.
		resp, err := s.BroadcastMessages(
			ctx,
			chainA,
			controllerAccount,
			submitMsg,
		)

		s.AssertValidTxResponse(resp)
		s.Require().NoError(err)

		balance, err := chainB.GetBalance(ctx, chainBAccount.Bech32Address(chainB.Config().Bech32Prefix), chainB.Config().Denom)
		s.Require().NoError(err)

		expected := testvalues.StartingTokenAmount
		s.Require().Equal(expected, balance)
	})

	t.Run("success: execute bank transfer tx from host account to another account on host chain thru controller account over ICA", func(t *testing.T) {

		// fund the host account account so it has some $$ to send
		err := chainB.SendFunds(ctx, ibctest.FaucetAccountKeyName, ibc.WalletAmount{
			Address: hostAccount,
			Amount:  testvalues.StartingTokenAmount,
			Denom:   chainB.Config().Denom,
		})
		s.Require().NoError(err)

		// assemble bank transfer message from host account to user account on host chain
		transferMsg := &banktypes.MsgSend{
			FromAddress: hostAccount,
			ToAddress:   chainBAccount.Bech32Address(chainB.Config().Bech32Prefix),
			Amount:      sdk.NewCoins(testvalues.DefaultTransferAmount(chainB.Config().Denom)),
		}

		// assemble submitMessage tx for intertx
		submitMsg, err := intertxtypes.NewMsgSubmitTx(
			transferMsg,
			connectionId,
			controllerAccount.Bech32Address(chainA.Config().Bech32Prefix),
		)
		s.Require().NoError(err)

		// broadcast submitMessage tx from controller account on chain A
		// this message should trigger the sending of an ICA packet over channel-1 (channel created between controller and host)
		// this ICA packet contains the assembled bank transfer message from above, which will be executed by the host account on the host chain.
		resp, err := s.BroadcastMessages(
			ctx,
			chainA,
			controllerAccount,
			submitMsg,
		)

		s.AssertValidTxResponse(resp)
		s.Require().NoError(err)

		balance, err := chainB.GetBalance(ctx, chainBAccount.Bech32Address(chainB.Config().Bech32Prefix), chainB.Config().Denom)
		s.Require().NoError(err)

		_, err = chainB.GetBalance(ctx, hostAccount, chainB.Config().Denom)
		s.Require().NoError(err)

		expected := testvalues.IBCTransferAmount + testvalues.StartingTokenAmount
		s.Require().Equal(expected, balance)
	})
}
