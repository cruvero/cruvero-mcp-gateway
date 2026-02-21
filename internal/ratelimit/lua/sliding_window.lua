-- Sliding window rate limiter using sorted sets.
-- KEYS[1]: the rate limit key
-- ARGV[1]: current timestamp in microseconds
-- ARGV[2]: window size in microseconds
-- ARGV[3]: max allowed requests (burst)
-- ARGV[4]: unique member ID (timestamp + random)
-- ARGV[5]: TTL in milliseconds for key expiration
--
-- Returns: {allowed (0/1), remaining, retry_after_ms}

local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local member = ARGV[4]
local ttl_ms = tonumber(ARGV[5])

-- Remove expired entries outside the window.
local window_start = now - window
redis.call('ZREMRANGEBYSCORE', key, '-inf', window_start)

-- Count current entries in the window.
local count = redis.call('ZCARD', key)

if count < limit then
    -- Add new entry and set expiration.
    redis.call('ZADD', key, now, member)
    redis.call('PEXPIRE', key, ttl_ms)
    local remaining = limit - count - 1
    return {1, remaining, 0}
else
    -- Over limit: compute retry-after from oldest entry in window.
    local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
    local retry_after_ms = 0
    if #oldest >= 2 then
        local oldest_score = tonumber(oldest[2])
        retry_after_ms = math.ceil((oldest_score + window - now) / 1000)
        if retry_after_ms < 0 then
            retry_after_ms = 0
        end
    end
    return {0, 0, retry_after_ms}
end
