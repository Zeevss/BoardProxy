package serverconfig

type ChangeKind string

const (
	UpsertUser      ChangeKind = "upsert_user"
	RemoveUser      ChangeKind = "remove_user"
	SetUserEnabled  ChangeKind = "set_user_enabled"
	UpsertBoard     ChangeKind = "upsert_board"
	RemoveBoard     ChangeKind = "remove_board"
	SetBoardEnabled ChangeKind = "set_board_enabled"
)

type Change struct {
	ID      string
	Kind    ChangeKind
	Tag     string
	Enabled bool
	User    *User
	Board   *Board
}
