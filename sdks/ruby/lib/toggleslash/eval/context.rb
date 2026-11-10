# frozen_string_literal: true

require_relative 'bucket'
require_relative 'text'
require_relative 'validate'

module ToggleSlash
  module Eval
    # What a kind that is read and absent means, everywhere.
    DEFAULT_KIND = 'user'

    # The key and attributes of one kind, after normalising. `attributes` is a Hash or nil.
    KindContext = Struct.new(:key, :attributes)

    # A context in either shape of docs/evaluation.md, read as a map from kind to KindContext. A context is
    # a Hash; its keys may be Strings or Symbols (the spec's names are the String ones).
    module Context
      module_function

      # `hash[name]`, by the String name or else the Symbol one.
      def get(hash, name)
        return hash[name] if hash.key?(name)

        hash.fetch(name.to_sym, nil)
      end

      def read_entry(value)
        key = get(value, 'key')
        attributes = get(value, 'attributes')
        return nil unless key.is_a?(String) && !key.empty?

        key = Text.utf8(key)
        return KindContext.new(key, nil) if attributes.nil?

        Validate.record?(attributes) ? KindContext.new(key, attributes) : nil
      end

      def read_multi(value)
        contexts = get(value, 'contexts')
        return nil unless Validate.absent?(get(value, 'key')) && Validate.absent?(get(value, 'attributes'))
        return nil unless Validate.record?(contexts) && !contexts.empty?

        normalised = {}
        contexts.each do |name, raw|
          kind = name.is_a?(Symbol) ? name.to_s : name
          return nil unless Validate.kind?(kind) && Validate.record?(raw) && Validate.absent?(get(raw, 'kind'))

          entry = read_entry(raw)
          return nil if entry.nil?

          normalised[Text.utf8(kind)] = entry
        end
        normalised
      end

      # The context as a Hash from kind to KindContext, or nil when it is not valid.
      def normalise(value)
        return nil unless Validate.record?(value)

        kind = get(value, 'kind')
        kind = kind.to_s if kind.is_a?(Symbol)
        kind = DEFAULT_KIND if kind.nil?
        return read_multi(value) if kind == 'multi'
        return nil unless Validate.kind?(kind)

        entry = read_entry(value)
        entry.nil? ? nil : { Text.utf8(kind) => entry }
      end

      # The bucket for the key of `kind` (absent is `user`): the key as it is for `user`, `<kind>:<key>`
      # for any other kind, and 0 when the context does not have that kind.
      def kind_bucket(key, salt, kind, contexts)
        chosen = kind.nil? ? DEFAULT_KIND : kind
        context = contexts[chosen]
        return 0 if context.nil?

        context_key = chosen == DEFAULT_KIND ? context.key : "#{chosen}:#{context.key}"
        Bucket.bucket(key, salt, context_key)
      end
    end
  end
end
