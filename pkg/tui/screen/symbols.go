package screen

import "github.com/keshon/gektor/pkg/tui/components/cell"

// Separator uses the selected style for application-owned metadata.
func Separator() string { return cell.Separator(CurrentPalette().Modern) }
