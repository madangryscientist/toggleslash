# frozen_string_literal: true

require_relative 'http'

module ToggleSlash
  # Failures the SDK absorbs rather than raise at a caller. SystemExit, Interrupt and NoMemoryError are not
  # among them: those are the process's, not the SDK's.
  RECOVERABLE = [StandardError, ScriptError, SystemStackError].freeze

  # The options a client was built with, after the rules of the contract: a missing or non-numeric or
  # non-positive duration takes its default, and only `false` turns the stream off.
  Config = Struct.new(:target, :sdk_key, :stream, :poll_interval, :flush_interval, :request_timeout,
                      :idle_timeout, :clock, :random, keyword_init: true)

  DEFAULT_INTERVAL = 30.0
  DEFAULT_TIMEOUT = 10.0
  # The server sends a keep-alive every 25 seconds; this long without a byte and the stream is dead.
  DEFAULT_IDLE_TIMEOUT = 60.0

  # The seconds `value` names, or `default`.
  def self.duration(value, default)
    return default unless value.is_a?(Integer) || value.is_a?(Float)
    return default unless value.to_f.finite? && value.positive?

    value.to_f
  end

  # The base URL with trailing slashes removed, or the empty string for anything that is not a String.
  def self.base_url(value)
    value.is_a?(String) ? value.sub(%r{/+\z}, '') : ''
  end
end
