#!/bin/bash
# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Banner
echo -e "${BLUE}╔══════════════════════════════════════╗${NC}"
echo -e "${BLUE}║${NC}      ${CYAN}Notification Server Tests${NC}      ${BLUE}║${NC}"
echo -e "${BLUE}╚══════════════════════════════════════╝${NC}"
echo ""

# Run tests and capture output
echo -e "${YELLOW}Running tests...${NC}"
echo ""

# Run go test in src directory and capture both stdout and exit code
TEST_OUTPUT=$(cd src && go test -v 2>&1)
EXIT_CODE=$?

# Display the output with some filtering for readability
echo "$TEST_OUTPUT" | while IFS= read -r line; do
    if [[ $line == *"=== RUN"* ]]; then
        # Main test function
        if [[ $line == *"=== RUN   Test"* ]] && [[ $line != *"/"* ]]; then
            echo -e "${PURPLE}🧪 ${line#*RUN   }${NC}"
        # Sub-test
        elif [[ $line == *"/"* ]]; then
            subtest=$(echo "$line" | sed 's/.*RUN   [^/]*\//  ├─ /')
            echo -e "${CYAN}${subtest}${NC}"
        fi
    elif [[ $line == *"--- PASS:"* ]]; then
        if [[ $line == *"/"* ]]; then
            # Sub-test pass
            echo -e "${GREEN}  ✅ PASS${NC}"
        else
            # Main test pass
            test_name=$(echo "$line" | sed 's/--- PASS: //' | sed 's/ (.*//')
            echo -e "${GREEN}✅ $test_name PASSED${NC}"
        fi
    elif [[ $line == *"--- FAIL:"* ]]; then
        if [[ $line == *"/"* ]]; then
            # Sub-test fail
            echo -e "${RED}  ❌ FAIL${NC}"
        else
            # Main test fail
            test_name=$(echo "$line" | sed 's/--- FAIL: //' | sed 's/ (.*//')
            echo -e "${RED}❌ $test_name FAILED${NC}"
        fi
    elif [[ $line == *"FAIL"* ]] && [[ $line == *"exit status"* ]]; then
        # Skip this line, we'll handle it in summary
        continue
    elif [[ $line == *"PASS"* ]] || [[ $line == *"ok"* ]]; then
        # Skip, we'll handle in summary
        continue
    elif [[ $line =~ ^[[:space:]]*[a-zA-Z0-9_-]+\.go:[0-9]+: ]]; then
        # Test failure details
        echo -e "${RED}    $line${NC}"
    fi
done

echo ""
echo -e "${BLUE}═══════════════════════════════════════${NC}"

# Parse results for summary - Fixed counting logic
TOTAL_TESTS=$(echo "$TEST_OUTPUT" | grep "^=== RUN   Test" | grep -v "/" | wc -l)
PASSED_TESTS=$(echo "$TEST_OUTPUT" | grep "^--- PASS: Test" | grep -v "/" | wc -l)
FAILED_TESTS=$(echo "$TEST_OUTPUT" | grep "^--- FAIL: Test" | grep -v "/" | wc -l)

# Summary
echo -e "${YELLOW}📊 UNIT TEST SUMMARY${NC}"
echo -e "${BLUE}───────────────────────────────────────${NC}"
echo -e "Tests:          ${GREEN}$PASSED_TESTS passed${NC}, ${RED}$FAILED_TESTS failed${NC} (${TOTAL_TESTS} total)"

# Overall result
echo -e "${BLUE}───────────────────────────────────────${NC}"
if [ $EXIT_CODE -eq 0 ]; then
    echo -e "${GREEN}🎉 ALL TESTS PASSED! 🎉${NC}"
    # Extract runtime more reliably
    RUNTIME=$(echo "$TEST_OUTPUT" | grep "^ok" | tail -1 | awk '{print $NF}')
    if [ ! -z "$RUNTIME" ]; then
        echo -e "${CYAN}⏱️  Runtime: $RUNTIME${NC}"
    fi
else
    echo -e "${RED}💥 SOME TESTS FAILED 💥${NC}"
    echo -e "${YELLOW}Check the output above for details${NC}"
fi

echo -e "${BLUE}═══════════════════════════════════════${NC}"
echo ""

# Exit with the same code as go test
exit $EXIT_CODE