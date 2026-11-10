# frozen_string_literal: true

module ToggleSlash
  module Eval
    # Why a flag served what it did. `kind` is :OFF, :PREREQUISITE_FAILED, :TARGET_MATCH, :RULE_MATCH,
    # :FALLTHROUGH or :ERROR. `rule_index` is set for :RULE_MATCH, `flag_key` for :PREREQUISITE_FAILED and
    # `code` (an error code of the contract) for :ERROR; the rest are nil.
    Reason = Data.define(:kind, :rule_index, :flag_key, :code) do
      def initialize(kind:, rule_index: nil, flag_key: nil, code: nil)
        super
      end

      # The wire shape of the contract: string keys, camelCase, only the fields the kind carries.
      def wire
        shape = { 'kind' => kind.to_s }
        shape['ruleIndex'] = rule_index unless rule_index.nil?
        shape['flagKey'] = flag_key unless flag_key.nil?
        shape['code'] = code.to_s unless code.nil?
        shape
      end

      def error?
        kind == :ERROR
      end
    end

    # The answer to one evaluation: the served variation's JSON value (nil when nothing is served), its
    # index (nil when nothing is served) and the reason.
    Detail = Data.define(:value, :variation_index, :reason) do
      def wire
        { 'value' => value, 'variationIndex' => variation_index, 'reason' => reason.wire }
      end
    end

    Reason::OFF = Reason.new(kind: :OFF)
    Reason::TARGET_MATCH = Reason.new(kind: :TARGET_MATCH)
    Reason::FALLTHROUGH = Reason.new(kind: :FALLTHROUGH)

    module_function

    def error_reason(code)
      Reason.new(kind: :ERROR, code: code)
    end

    def failure(code)
      Detail.new(value: nil, variation_index: nil, reason: error_reason(code))
    end
  end
end
