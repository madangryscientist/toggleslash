# frozen_string_literal: true

module Toggleslash
  module Eval
    # Contexts: validation and the map from kind to [key, attributes] (evaluation.md, "Context").
    module Context
      DEFAULT_KIND = "user"
      KIND_BYTES = ((65..90).to_a + (97..122).to_a + (48..57).to_a + [46, 95, 45]).freeze # A-Z a-z 0-9 . _ -

      # The value under `name` in a Hash keyed by Strings or Symbols, or nil.
      def self.get(map, name)
        return map[name] if map.key?(name)

        symbol = begin
          name.to_sym
        rescue EncodingError
          nil
        end
        symbol.nil? ? nil : map[symbol]
      end

      def self.has?(map, name)
        map.key?(name) || begin
          map.key?(name.to_sym)
        rescue EncodingError
          false
        end
      end

      # A kind is one or more ASCII letters, digits, ".", "_" or "-", and not "kind" or "multi".
      def self.kind?(value)
        return false unless value.is_a?(String) && !value.empty? && value != "kind" && value != "multi"

        value.ascii_only? && value.bytes.all? { |b| KIND_BYTES.include?(b) }
      end

      def self.entry(raw)
        return nil unless raw.is_a?(Hash)

        key = get(raw, "key")
        return nil unless key.is_a?(String) && !key.empty?

        attributes = get(raw, "attributes")
        return nil unless attributes.nil? || attributes.is_a?(Hash)

        [Text.utf8(key), attributes]
      end

      # The context as { kind => [key, attributes] }, or nil when it is not valid.
      def self.normalise(context)
        return nil unless context.is_a?(Hash)

        kind = get(context, "kind")
        kind = DEFAULT_KIND if kind.nil?
        return nil unless kind.is_a?(String)

        kind = Text.utf8(kind)
        return multi(context) if kind == "multi"
        return nil unless kind?(kind)

        entry = entry(context)
        entry.nil? ? nil : { kind => entry }
      end

      def self.multi(context)
        return nil unless get(context, "key").nil? && get(context, "attributes").nil?

        contexts = get(context, "contexts")
        return nil unless contexts.is_a?(Hash) && !contexts.empty?

        out = {}
        contexts.each do |name, raw|
          name = name.to_s if name.is_a?(Symbol)
          return nil unless kind?(name) && raw.is_a?(Hash) && get(raw, "kind").nil?

          entry = entry(raw)
          return nil if entry.nil?

          out[name] = entry
        end
        out
      end
    end
  end
end
