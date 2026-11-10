# frozen_string_literal: true

$LOAD_PATH.unshift(File.expand_path('../lib', __dir__))

require 'minitest/autorun'
require 'toggleslash'

# Ruby's analogue of `-W error`: a warning anywhere (the SDK, a test) fails the run.
module FailOnWarning
  def warn(message, *_rest, **_options)
    raise "Ruby warning: #{message}"
  end
end
Warning.singleton_class.prepend(FailOnWarning)

REPO_ROOT = File.expand_path('../../..', __dir__)

module TestSupport
  # Two parsed JSON values are the same by the spec's equality: numbers by IEEE-754 (so -0 equals 0 and
  # 1 equals 1.0), strings byte for byte, objects by their members.
  def same_json?(left, right)
    case left
    when Hash
      right.is_a?(Hash) && left.keys.sort == right.keys.sort && left.all? { |key, value| same_json?(value, right[key]) }
    when Array
      right.is_a?(Array) && left.length == right.length && left.zip(right).all? { |a, b| same_json?(a, b) }
    when Integer, Float
      (right.is_a?(Integer) || right.is_a?(Float)) && left.to_f == right.to_f # rubocop:disable Lint/FloatComparison
    else
      left == right
    end
  end
end
