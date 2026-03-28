#!/bin/bash

# Test Script for Multi-Tenant Messaging System
# Usage: ./test.sh [unit|integration|all]

set -e

echo "🧪 Multi-Tenant Messaging System - Test Runner"
echo "============================================="
echo ""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print status
print_status() {
    echo -e "${GREEN}✓${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

# Check if docker is running
check_docker() {
    if ! docker info > /dev/null 2>&1; then
        print_error "Docker is not running. Please start Docker first."
        exit 1
    fi
    print_status "Docker is running"
}

# Check Go version
check_go() {
    if ! command -v go &> /dev/null; then
        print_error "Go is not installed"
        exit 1
    fi
    
    GO_VERSION=$(go version | awk '{print $3}')
    print_status "Go version: $GO_VERSION"
}

# Run unit tests
run_unit_tests() {
    echo ""
    echo "📦 Running Unit Tests..."
    echo "------------------------"
    
    # Test compilation
    if go build -o /dev/null ./cmd/api 2>&1 | grep -q "error"; then
        print_error "Build failed"
        go build ./cmd/api
        exit 1
    fi
    print_status "Build successful"
    
    # Run go vet
    if go vet ./... 2>&1 | grep -v "^#"; then
        print_warning "go vet found issues"
    else
        print_status "go vet passed"
    fi
    
    # Run unit tests (skip integration tests)
    if go test -v -short ./... 2>&1; then
        print_status "Unit tests passed"
    else
        print_error "Unit tests failed"
        exit 1
    fi
}

# Run integration tests
run_integration_tests() {
    echo ""
    echo "🔥 Running Integration Tests..."
    echo "-------------------------------"
    
    check_docker
    
    # Start services
    echo "Starting PostgreSQL and RabbitMQ..."
    docker-compose up -d postgres rabbitmq
    
    # Wait for services
    echo "Waiting for services to be ready..."
    sleep 5
    
    # Run migrations
    echo "Running database migrations..."
    docker-compose exec -T postgres psql -U user -d multitenant -f /dev/stdin < internal/database/migrations/001_create_tenants_table.sql 2>/dev/null || true
    docker-compose exec -T postgres psql -U user -d multitenant -f /dev/stdin < internal/database/migrations/002_create_messages_partitioned.sql 2>/dev/null || true
    docker-compose exec -T postgres psql -U user -d multitenant -f /dev/stdin < internal/database/migrations/003_create_tenant_partition_trigger.sql 2>/dev/null || true
    docker-compose exec -T postgres psql -U user -d multitenant -f /dev/stdin < internal/database/migrations/004_create_dlx_infrastructure.sql 2>/dev/null || true
    
    # Run integration tests
    if go test -v -tags=integration ./internal/test/... 2>&1; then
        print_status "Integration tests passed"
    else
        print_error "Integration tests failed"
        docker-compose down
        exit 1
    fi
    
    # Cleanup
    docker-compose down
}

# Manual API test with curl
run_api_tests() {
    echo ""
    echo "🌐 Running API Tests with curl..."
    echo "----------------------------------"
    
    # Start the server in background
    echo "Starting server..."
    JWT_SECRET=test-secret make run &
    SERVER_PID=$!
    
    # Wait for server
    sleep 3
    
    # Trap to kill server on exit
    trap "kill $SERVER_PID 2>/dev/null || true" EXIT
    
    BASE_URL="http://localhost:8080/api/v1"
    
    # Test health
    echo "Testing health endpoint..."
    if curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/../health" | grep -q "200"; then
        print_status "Health check passed"
    else
        print_error "Health check failed"
        exit 1
    fi
    
    # Login and get token
    echo "Testing authentication..."
    TOKEN=$(curl -s -X POST "$BASE_URL/auth/login" \
        -H "Content-Type: application/json" \
        -d '{"username":"admin","password":"password"}' | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
    
    if [ -z "$TOKEN" ]; then
        print_error "Failed to get JWT token"
        exit 1
    fi
    print_status "JWT token acquired"
    
    # Create tenant
    echo "Testing tenant creation..."
    TENANT_RESPONSE=$(curl -s -X POST "$BASE_URL/tenants" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $TOKEN" \
        -d '{"name":"test-tenant","workers":3}')
    
    TENANT_ID=$(echo "$TENANT_RESPONSE" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
    
    if [ -z "$TENANT_ID" ]; then
        print_error "Failed to create tenant"
        echo "Response: $TENANT_RESPONSE"
        exit 1
    fi
    print_status "Tenant created: $TENANT_ID"
    
    # Get tenant
    echo "Testing get tenant..."
    if curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/tenants/$TENANT_ID" \
        -H "Authorization: Bearer $TOKEN" | grep -q "200"; then
        print_status "Get tenant passed"
    else
        print_error "Get tenant failed"
        exit 1
    fi
    
    # Update concurrency
    echo "Testing concurrency update..."
    if curl -s -o /dev/null -w "%{http_code}" -X PUT "$BASE_URL/tenants/$TENANT_ID/config/concurrency" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $TOKEN" \
        -d '{"workers":5}' | grep -q "200"; then
        print_status "Concurrency update passed"
    else
        print_error "Concurrency update failed"
        exit 1
    fi
    
    # Create message
    echo "Testing message creation..."
    MSG_RESPONSE=$(curl -s -X POST "$BASE_URL/messages" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer $TOKEN" \
        -d "{\"tenant_id\":\"$TENANT_ID\",\"payload\":{\"event\":\"test\"}}")
    
    MSG_ID=$(echo "$MSG_RESPONSE" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
    
    if [ -z "$MSG_ID" ]; then
        print_error "Failed to create message"
        echo "Response: $MSG_RESPONSE"
        exit 1
    fi
    print_status "Message created: $MSG_ID"
    
    # List messages
    echo "Testing message listing..."
    if curl -s "$BASE_URL/messages?tenant_id=$TENANT_ID&limit=10" \
        -H "Authorization: Bearer $TOKEN" | grep -q "data"; then
        print_status "Message listing passed"
    else
        print_error "Message listing failed"
        exit 1
    fi
    
    # Delete tenant
    echo "Testing tenant deletion..."
    if curl -s -o /dev/null -w "%{http_code}" -X DELETE "$BASE_URL/tenants/$TENANT_ID" \
        -H "Authorization: Bearer $TOKEN" | grep -q "204"; then
        print_status "Tenant deletion passed"
    else
        print_error "Tenant deletion failed"
        exit 1
    fi
    
    # Kill server
    kill $SERVER_PID 2>/dev/null || true
    
    print_status "All API tests passed!"
}

# Show test coverage
show_coverage() {
    echo ""
    echo "📊 Test Coverage..."
    echo "-------------------"
    go test -cover ./... 2>&1 | grep -E "coverage|ok"
}

# Main execution
main() {
    check_go
    
    case "${1:-all}" in
        unit)
            run_unit_tests
            ;;
        integration)
            run_integration_tests
            ;;
        api)
            run_api_tests
            ;;
        coverage)
            show_coverage
            ;;
        all)
            run_unit_tests
            show_coverage
            print_status "All tests completed!"
            echo ""
            echo "To run integration tests: ./test.sh integration"
            echo "To run API tests: ./test.sh api"
            ;;
        *)
            echo "Usage: $0 [unit|integration|api|coverage|all]"
            exit 1
            ;;
    esac
}

main "$@"
