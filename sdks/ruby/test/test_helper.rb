# frozen_string_literal: true

$LOAD_PATH.unshift File.expand_path("../lib", __dir__)

# A warning from the SDK or its tests is a failure: Ruby only warns, so the tests make it raise.
module WarningsAreErrors
  def warn(message, category: nil, **)
    raise "Ruby warning: #{message}" unless message.include?("minitest") || message.include?("/gems/")

    super
  end
end
Warning.singleton_class.prepend(WarningsAreErrors)

require "minitest"
require "minitest/autorun"
require "toggleslash"

module TestSupport
  REPO = File.expand_path("../../..", __dir__)
  FIXTURES = File.join(REPO, "fixtures")
end
