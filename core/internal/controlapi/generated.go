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

var NewControlServiceClient = controlv1.NewControlServiceClient
var RegisterControlServiceServer = controlv1.RegisterControlServiceServer
