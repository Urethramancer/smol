package main

import (
	"fmt"
	"os"
	"strings"

	smol "github.com/Urethramancer/smol"
	"github.com/grimdork/climate/arg"
	"github.com/grimdork/climate/human"
)

func ahumanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GiB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.2f MiB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.2f KiB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

const (
	flagsGroup   = "Flags"
	compFlag     = "compress"
	decompFlag   = "decompress"
	levelOption  = "level"
	outputOption = "out"
	inputPos     = "INPUT"
)

func main() {
	opt := arg.New("smol", "A small experimental compression tool.")
	opt.SetDefaultHelp(true)
	opt.SetFlag(flagsGroup, "c", compFlag, "Compress mode.")
	opt.SetFlag(flagsGroup, "d", decompFlag, "Decompress mode.")
	opt.SetOption("Options", "l", levelOption, "Compression level.", "normal", false, arg.VarString, []any{"fast", "normal", "high"})
	opt.SetOption("Options", "o", outputOption, "Output filename (default:<input>.smol).", "", false, arg.VarString, nil)
	opt.SetPositional(inputPos, "File to compress.", "", true, arg.VarString)
	err := opt.Parse(os.Args[1:])
	if err != nil {
		opt.PrintHelp()
		os.Exit(1)
	}

	compressFlag := opt.GetBool(compFlag)
	decompressFlag := opt.GetBool(decompFlag)
	level := opt.GetString(levelOption)
	output := opt.GetString(outputOption)
	inputFile := opt.GetPosString(inputPos)

	po := smol.ParsingOptions{
		UseOptimal: true,
		ChainDepth: 512,
	}
	switch level {
	case "fast":
		po.UseOptimal = false
		po.ChainDepth = 256
	case "normal":
	case "high":
		po.ChainDepth = 4096
	}

	// Require exactly one of -c or -d and one positional input file.
	if compressFlag == decompressFlag {
		fmt.Println("Specify exactly one of -c (compress) or -d (decompress)")
		opt.PrintHelp()
		os.Exit(1)
	}

	if output == "" {
		if compressFlag {
			output = inputFile + ".smol"
		} else {
			if strings.HasSuffix(inputFile, ".smol") {
				output = strings.TrimSuffix(inputFile, ".smol")
			} else {
				output = inputFile + ".out"
			}
		}
	}

	if compressFlag {
		inStat, err := os.Stat(inputFile)
		if err != nil {
			fmt.Printf("Error stat'ing input file: %v\n", err)
			return
		}
		inSize := inStat.Size()

		fmt.Printf("Compressing '%s' to '%s'...\n", inputFile, output)

		in, err := os.Open(inputFile)
		if err != nil {
			fmt.Printf("Error opening input file: %v\n", err)
			return
		}
		defer in.Close()

		out, err := os.Create(output)
		if err != nil {
			fmt.Printf("Error creating output file: %v\n", err)
			return
		}
		defer out.Close()

		if err := smol.Compress(in, out, po); err != nil {
			fmt.Printf("Error during compression: %v\n", err)
			os.Remove(output)
			return
		}

		// Ensure data is flushed to disk and stat the output file for size reporting.
		if fi, err := out.Stat(); err == nil {
			out.Sync()
			outSize := fi.Size()
			ratio := float64(outSize) / float64(inSize)
			hin := human.UInt(uint64(inSize), true)
			hout := human.UInt(uint64(outSize), true)
			fmt.Printf("Compression complete: %s → %s (ratio %.2f, %s → %s)\n",
				inputFile, output, ratio, hin, hout)
		} else {
			fmt.Println("Compression complete (unable to stat output for size)")
		}
	}

	if decompressFlag {
		inStat, err := os.Stat(inputFile)
		if err != nil {
			fmt.Printf("Error stat'ing input file: %v\n", err)
			return
		}
		inSize := inStat.Size()

		fmt.Printf("Decompressing '%s' to '%s'...\n", inputFile, output)

		in, err := os.Open(inputFile)
		if err != nil {
			fmt.Printf("Error opening input file: %v\n", err)
			return
		}
		defer in.Close()

		out, err := os.Create(output)
		if err != nil {
			fmt.Printf("Error creating output file: %v\n", err)
			return
		}
		defer out.Close()

		if err := smol.Decompress(in, out); err != nil {
			fmt.Printf("Error during decompression: %v\n", err)
			os.Remove(output)
			return
		}

		// Report sizes for decompression too
		if fi, err := out.Stat(); err == nil {
			out.Sync()
			outSize := fi.Size()
			ratio := float64(inSize) / float64(outSize)
			hin := human.UInt(uint64(inSize), true)
			hout := human.UInt(uint64(outSize), true)
			fmt.Printf("Decompression complete: %s → %s (ratio %.2f, %s → %s)\n", inputFile, output, ratio, hin, hout)
		} else {
			fmt.Println("Decompression complete (unable to stat output for size)")
		}
	}
}
