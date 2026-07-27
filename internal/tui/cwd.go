package tui

import "os"

// getCwd es un alias envolvente de os.Getwd para poder reemplazarlo en tests
// (por ejemplo cuando queremos anclar la búsqueda de skills a un directorio
// temporal).
var getCwd = os.Getwd
