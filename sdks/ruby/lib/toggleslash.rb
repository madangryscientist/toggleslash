# frozen_string_literal: true

require_relative "toggleslash/types"
require_relative "toggleslash/text"
require_relative "toggleslash/json"
require_relative "toggleslash/eval/num"
require_relative "toggleslash/eval/context"
require_relative "toggleslash/eval/reference"
require_relative "toggleslash/eval/semver"
require_relative "toggleslash/eval/dates"
require_relative "toggleslash/eval/bucket"
require_relative "toggleslash/eval/pattern"
require_relative "toggleslash/eval/ruleset"
require_relative "toggleslash/eval/evaluator"
require_relative "toggleslash/event"
require_relative "toggleslash/sse"
require_relative "toggleslash/http"
require_relative "toggleslash/usage"
require_relative "toggleslash/client"

# The ToggleSlash server SDK for Ruby. See README.md for the mapping from the contract.
module Toggleslash
  # Starts a client: two background threads, and the first ruleset request at once. It returns at once;
  # call `client.ready` to wait for the first answer.
  #
  # @param url [String] the server's base URL
  # @param sdk_key [String] a server SDK key
  # @param stream [Boolean] false turns the stream off and polls instead (default true)
  # @param poll_interval [Numeric] seconds between polls (default 30)
  # @param flush_interval [Numeric] seconds between usage flushes (default 30)
  # @param request_timeout [Numeric] seconds a whole request may take (default 10)
  # @param hooks [Hooks, Hash, nil] `after_evaluation` is called after every evaluation
  # @param on_error [#call, nil] receives a {Toggleslash::Error} (or what a hook raised)
  # @return [Client]
  def self.create(url:, sdk_key:, stream: true, poll_interval: Client::DEFAULT_POLL,
                  flush_interval: Client::DEFAULT_FLUSH, request_timeout: Client::DEFAULT_TIMEOUT,
                  hooks: nil, on_error: nil)
    Client.new(url: url, sdk_key: sdk_key, stream: stream, poll_interval: poll_interval,
               flush_interval: flush_interval, request_timeout: request_timeout, hooks: hooks,
               on_error: on_error).start
  end
end
