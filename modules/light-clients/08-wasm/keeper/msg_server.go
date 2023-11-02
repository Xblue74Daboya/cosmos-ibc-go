package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/ibc-go/modules/light-clients/08-wasm/internal/ibcwasm"
	"github.com/cosmos/ibc-go/modules/light-clients/08-wasm/types"
	ibcerrors "github.com/cosmos/ibc-go/v8/modules/core/errors"
)

var _ types.MsgServer = (*Keeper)(nil)

// StoreCode defines a rpc handler method for MsgStoreCode
func (k Keeper) StoreCode(goCtx context.Context, msg *types.MsgStoreCode) (*types.MsgStoreCodeResponse, error) {
	if k.GetAuthority() != msg.Signer {
		return nil, errorsmod.Wrapf(ibcerrors.ErrUnauthorized, "expected %s, got %s", k.GetAuthority(), msg.Signer)
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	codeHash, err := k.storeWasmCode(ctx, msg.WasmByteCode)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to store wasm bytecode")
	}

	emitStoreWasmCodeEvent(ctx, codeHash)

	return &types.MsgStoreCodeResponse{
		Checksum: codeHash,
	}, nil
}

// RemoveCodeHash defines a rpc handler method for MsgRemoveCodeHash
func (k Keeper) RemoveCodeHash(goCtx context.Context, msg *types.MsgRemoveCodeHash) (*types.MsgRemoveCodeHashResponse, error) {
	if k.GetAuthority() != msg.Signer {
		return nil, errorsmod.Wrapf(ibcerrors.ErrUnauthorized, "expected %s, got %s", k.GetAuthority(), msg.Signer)
	}

	found := types.HasCodeHash(goCtx, msg.CodeHash)

	err := ibcwasm.CodeHashes.Remove(goCtx, msg.CodeHash)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove code hash")
	}

	return &types.MsgRemoveCodeHashResponse{Found: found}, nil
}
