# frozen_string_literal: true

module Toggleslash
  # The closed sets keep the contract's spelling: each constant is the plain frozen string.
  module ReasonKind
    OFF = "OFF"
    PREREQUISITE_FAILED = "PREREQUISITE_FAILED"
    TARGET_MATCH = "TARGET_MATCH"
    RULE_MATCH = "RULE_MATCH"
    FALLTHROUGH = "FALLTHROUGH"
    ERROR = "ERROR"
    ALL = [OFF, PREREQUISITE_FAILED, TARGET_MATCH, RULE_MATCH, FALLTHROUGH, ERROR].freeze
  end

  module ErrorCode
    NOT_READY = "NOT_READY"
    INVALID_CONTEXT = "INVALID_CONTEXT"
    FLAG_NOT_FOUND = "FLAG_NOT_FOUND"
    SEGMENT_NOT_FOUND = "SEGMENT_NOT_FOUND"
    MALFORMED_FLAG = "MALFORMED_FLAG"
    MALFORMED_SEGMENT = "MALFORMED_SEGMENT"
    PREREQUISITE_CYCLE = "PREREQUISITE_CYCLE"
    PREREQUISITE_DEPTH_EXCEEDED = "PREREQUISITE_DEPTH_EXCEEDED"
    TYPE_MISMATCH = "TYPE_MISMATCH"
    EVALUATION_FAILED = "EVALUATION_FAILED"
    ALL = [
      NOT_READY, INVALID_CONTEXT, FLAG_NOT_FOUND, SEGMENT_NOT_FOUND, MALFORMED_FLAG, MALFORMED_SEGMENT,
      PREREQUISITE_CYCLE, PREREQUISITE_DEPTH_EXCEEDED, TYPE_MISMATCH, EVALUATION_FAILED
    ].freeze
  end

  module StreamState
    OFF = "off"
    CONNECTING = "connecting"
    CONNECTED = "connected"
    DISCONNECTED = "disconnected"
    CLOSED = "closed"
    ALL = [OFF, CONNECTING, CONNECTED, DISCONNECTED, CLOSED].freeze
  end

  # The `lastError` strings of a status. `http_<status>` is built by `http`.
  module LastError
    TIMEOUT = "timeout"
    NETWORK = "network"
    BAD_RULESET = "bad_ruleset"
    UNAUTHORIZED = "http_401"
    # The contract's `http_<status>` is built by this helper.
    def self.http(status)
      "http_#{status}"
    end
  end

  # Why a flag evaluated the way it did. `code` is set for an ERROR, `rule_index` for a RULE_MATCH and
  # `flag_key` for a PREREQUISITE_FAILED.
  Reason = Data.define(:kind, :code, :rule_index, :flag_key) do
    def initialize(kind:, code: nil, rule_index: nil, flag_key: nil)
      super
    end
  end

  # The answer of `variation_detail`.
  Detail = Data.define(:value, :variation_index, :reason)

  # One snapshot of the client's state. `last_sync_at` is a UTC `Time` or nil.
  Status = Data.define(:etag, :last_sync_at, :stream, :last_error)

  # What `hooks.after_evaluation` receives.
  EvaluationEvent = Data.define(:flag_key, :context, :value, :variation_index, :reason, :etag, :flag_version)

  # The hook set: `Hooks.new(after_evaluation: callable)`.
  Hooks = Data.define(:after_evaluation) do
    def initialize(after_evaluation: nil)
      super
    end
  end

  # What `on_error` receives.
  class Error < StandardError
    attr_reader :code

    def initialize(message, code = nil)
      super(message)
      @code = code
    end
  end
end
