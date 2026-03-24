BINDIR=bin
TINYGO:=tinygo
TIME=time
CMP=cmp

GOTOOLCHAIN:=go1.25.8
FLAGS:=-no-debug -panic=trap -scheduler=none
BINARY:=smol
SRCDIR:=cmd/smol

.PHONY: all build gobuild clean test gotest tinygo-test bench microbench roundtrip

all: build

build: $(SRCDIR)/main.go
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(TINYGO) build $(FLAGS) -o $(BINDIR)/$(BINARY) ./$(SRCDIR)
	@strip $(BINDIR)/$(BINARY) || @echo "Strip failed."

test: build
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(TINYGO) test .

bench:
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(TINYGO) test -run '' -bench . -benchmem .

microbench:
	GOTOOLCHAIN=$(GOTOOLCHAIN) $(TINYGO) test -run TestBuildFastTableMicro -v

roundtrip: build
	@./$(BINDIR)/$(BINARY) -c example.txt -o example.txt.smol
	@./$(BINDIR)/$(BINARY) -d example.txt.smol -o example.txt.out
	@$(CMP) example.txt example.txt.out && echo "roundtrip OK" || echo "roundtrip failed"

clean:
	rm -f $(BINDIR)/$(BINARY) $(BINDIR)/$(BINARY)_go *.smol *.out
