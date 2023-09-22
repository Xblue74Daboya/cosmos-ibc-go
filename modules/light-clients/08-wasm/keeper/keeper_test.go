package keeper_test

import (
	"fmt"
	"os"
	"testing"

	wasmvm "github.com/CosmWasm/wasmvm"
	testifysuite "github.com/stretchr/testify/suite"

	"github.com/cosmos/cosmos-sdk/baseapp"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/cosmos/ibc-go/modules/light-clients/08-wasm/keeper"
	"github.com/cosmos/ibc-go/modules/light-clients/08-wasm/types"
	ibctesting "github.com/cosmos/ibc-go/v7/testing"
)

type KeeperTestSuite struct {
	testifysuite.Suite

	coordinator *ibctesting.Coordinator

	// testing chains used for convenience and readability
	chainA *ibctesting.TestChain
	chainB *ibctesting.TestChain
	chainC *ibctesting.TestChain
}

func (suite *KeeperTestSuite) SetupTest() {
	suite.coordinator = ibctesting.NewCoordinator(suite.T(), 3)
	suite.chainA = suite.coordinator.GetChain(ibctesting.GetChainID(1))
	suite.chainB = suite.coordinator.GetChain(ibctesting.GetChainID(2))
	suite.chainC = suite.coordinator.GetChain(ibctesting.GetChainID(3))

	queryHelper := baseapp.NewQueryServerTestHelper(suite.chainA.GetContext(), suite.chainA.GetSimApp().InterfaceRegistry())
	types.RegisterQueryServer(queryHelper, suite.chainA.GetSimApp().WasmClientKeeper)
}

func TestKeeperTestSuite(t *testing.T) {
	testifysuite.Run(t, new(KeeperTestSuite))
}

func (suite *KeeperTestSuite) TestIterateCode() {
	testCases := []struct {
		name      string
		wasmFiles []string
	}{
		{
			name:      "single contract",
			wasmFiles: []string{"../test_data/ics10_grandpa_cw.wasm.gz"},
		},

		{
			name:      "multiple contract",
			wasmFiles: []string{"../test_data/ics07_tendermint_cw.wasm.gz", "../test_data/ics10_grandpa_cw.wasm.gz"},
		},
	}

	for _, tc := range testCases {
		tc := tc
		suite.SetupTest()

		suite.Run(tc.name, func() {
			var expectedAllCodeHash []byte
			for _, contractDir := range tc.wasmFiles {
				signer := authtypes.NewModuleAddress(govtypes.ModuleName).String()
				code, _ := os.ReadFile(contractDir)
				msg := types.NewMsgStoreCode(signer, code)

				ctx := suite.chainA.GetContext()
				_, err := suite.chainA.GetSimApp().WasmClientKeeper.StoreCode(ctx, msg)
				suite.NoError(err)
				var hashCode []byte
				if types.IsGzip(code) {
					code, err = types.Uncompress(code, types.MaxWasmByteSize())
					suite.NoError(err)
					hashCode = keeper.GenerateWasmCodeHash(code)
				}
				expectedAllCodeHash = append(expectedAllCodeHash, hashCode...)
			}

			var allCodeHash []byte
			suite.chainA.GetSimApp().WasmClientKeeper.IterateCode(
				suite.chainA.GetContext(), func(b []byte) bool {
					allCodeHash = append(allCodeHash, keeper.GenerateWasmCodeHash(b)...)
					return false
				},
			)

			suite.Equal(expectedAllCodeHash, allCodeHash)
		})
	}
}

func (suite *KeeperTestSuite) TestNewKeeper() {
	testCases := []struct {
		name          string
		instantiateFn func()
		expPass       bool
		expError      error
	}{
		{
			"success",
			func() {
				keeper.NewKeeperWithVM(
					suite.chainA.GetSimApp().AppCodec(),
					suite.chainA.GetSimApp().GetKey(types.StoreKey),
					suite.chainA.GetSimApp().WasmClientKeeper.GetAuthority(),
					types.WasmVM,
				)
			},
			true,
			nil,
		},
		{
			"failure: empty authority",
			func() {
				keeper.NewKeeperWithVM(
					suite.chainA.GetSimApp().AppCodec(),
					suite.chainA.GetSimApp().GetKey(types.StoreKey),
					"", // authority
					types.WasmVM,
				)
			},
			false,
			fmt.Errorf("authority must be non-empty"),
		},
		{
			"failure: nil wasm VM",
			func() {
				keeper.NewKeeperWithVM(
					suite.chainA.GetSimApp().AppCodec(),
					suite.chainA.GetSimApp().GetKey(types.StoreKey),
					suite.chainA.GetSimApp().WasmClientKeeper.GetAuthority(),
					nil,
				)
			},
			false,
			fmt.Errorf("wasm VM must be not nil"),
		},
		{
			"failure: different VM instances",
			func() {
				vm, err := wasmvm.NewVM("", "", 16, true, 64)
				suite.Require().NoError(err)

				keeper.NewKeeperWithVM(
					suite.chainA.GetSimApp().AppCodec(),
					suite.chainA.GetSimApp().GetKey(types.StoreKey),
					suite.chainA.GetSimApp().WasmClientKeeper.GetAuthority(),
					vm,
				)
			},
			false,
			fmt.Errorf("global Wasm VM instance should not be set to a different instance"),
		},
	}

	for _, tc := range testCases {
		tc := tc
		suite.SetupTest()

		suite.Run(tc.name, func() {
			if tc.expPass {
				suite.Require().NotPanics(
					tc.instantiateFn,
				)
			} else {
				suite.Require().PanicsWithError(tc.expError.Error(), func() {
					tc.instantiateFn()
				})
			}
		})
	}
}
