// Package controlapi implements the server-side adapter for the public gRPC
// contract. Generated wire types live under api/control/v1 so external control
// planes can import them without depending on core internals.
package controlapi

import controlv1 "bproxy-core/api/control/v1"

type ControlServiceClient = controlv1.ControlServiceClient
type ControlServiceServer = controlv1.ControlServiceServer
type UnimplementedControlServiceServer = controlv1.UnimplementedControlServiceServer
type UnsafeControlServiceServer = controlv1.UnsafeControlServiceServer

type RuntimeInfo = controlv1.RuntimeInfo
type RevisionRequest = controlv1.RevisionRequest
type ResourceRequest = controlv1.ResourceRequest
type MutationResult = controlv1.MutationResult
type SetEnabledRequest = controlv1.SetEnabledRequest
type UserSpec = controlv1.UserSpec
type AddUserRequest = controlv1.AddUserRequest
type ReplaceUserRequest = controlv1.ReplaceUserRequest
type ApplySnapshotRequest = controlv1.ApplySnapshotRequest
type ApplyChangesRequest = controlv1.ApplyChangesRequest
type ApplyChangesResult = controlv1.ApplyChangesResult
type ResourceChange = controlv1.ResourceChange
type ResourceChange_UpsertUser = controlv1.ResourceChange_UpsertUser
type ResourceChange_RemoveUser = controlv1.ResourceChange_RemoveUser
type ResourceChange_SetUserEnabled = controlv1.ResourceChange_SetUserEnabled
type ResourceChange_UpsertBoard = controlv1.ResourceChange_UpsertBoard
type ResourceChange_RemoveBoard = controlv1.ResourceChange_RemoveBoard
type ResourceChange_SetBoardEnabled = controlv1.ResourceChange_SetBoardEnabled
type ResourceTag = controlv1.ResourceTag
type ResourceEnabled = controlv1.ResourceEnabled
type WatchRuntimeEventsRequest = controlv1.WatchRuntimeEventsRequest
type CoreRuntimeEvent = controlv1.CoreRuntimeEvent
type CoreRuntimeEvent_ResourceChanged = controlv1.CoreRuntimeEvent_ResourceChanged
type CoreRuntimeEvent_BoardStateChanged = controlv1.CoreRuntimeEvent_BoardStateChanged
type CoreRuntimeEvent_ClientSessionOpened = controlv1.CoreRuntimeEvent_ClientSessionOpened
type CoreRuntimeEvent_ClientSessionClosed = controlv1.CoreRuntimeEvent_ClientSessionClosed
type CoreRuntimeEvent_StreamReset = controlv1.CoreRuntimeEvent_StreamReset
type ResourceChanged = controlv1.ResourceChanged
type BoardStateChanged = controlv1.BoardStateChanged
type ClientSessionOpened = controlv1.ClientSessionOpened
type ClientSessionClosed = controlv1.ClientSessionClosed
type EventStreamReset = controlv1.EventStreamReset
type RuntimeSnapshot = controlv1.RuntimeSnapshot
type UserInfo = controlv1.UserInfo
type ListUsersResponse = controlv1.ListUsersResponse
type KeylinkResponse = controlv1.KeylinkResponse
type BoardSpec = controlv1.BoardSpec
type AddBoardRequest = controlv1.AddBoardRequest
type ReplaceBoardRequest = controlv1.ReplaceBoardRequest
type BoardInfo = controlv1.BoardInfo
type ListBoardsResponse = controlv1.ListBoardsResponse
type RuntimeStats = controlv1.RuntimeStats
type UserRuntimeStats = controlv1.UserRuntimeStats
type BoardRuntimeStats = controlv1.BoardRuntimeStats
type TransportStats = controlv1.TransportStats

type ResourceKind = controlv1.ResourceKind
type ResourceOperation = controlv1.ResourceOperation

const (
	ResourceKind_RESOURCE_KIND_USER               = controlv1.ResourceKind_RESOURCE_KIND_USER
	ResourceKind_RESOURCE_KIND_BOARD              = controlv1.ResourceKind_RESOURCE_KIND_BOARD
	ResourceOperation_RESOURCE_OPERATION_ADDED    = controlv1.ResourceOperation_RESOURCE_OPERATION_ADDED
	ResourceOperation_RESOURCE_OPERATION_UPDATED  = controlv1.ResourceOperation_RESOURCE_OPERATION_UPDATED
	ResourceOperation_RESOURCE_OPERATION_ENABLED  = controlv1.ResourceOperation_RESOURCE_OPERATION_ENABLED
	ResourceOperation_RESOURCE_OPERATION_DISABLED = controlv1.ResourceOperation_RESOURCE_OPERATION_DISABLED
	ResourceOperation_RESOURCE_OPERATION_REMOVED  = controlv1.ResourceOperation_RESOURCE_OPERATION_REMOVED
)

var NewControlServiceClient = controlv1.NewControlServiceClient
var RegisterControlServiceServer = controlv1.RegisterControlServiceServer
