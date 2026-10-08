import type {ThemeRegistrationRaw} from 'shiki/core'

// TextMate port of the Zed theme "Claude Code Inspired Dark" (its `syntax` map). Zed capture names are
// mapped to the closest standard TextMate scopes; the most specific matching scope wins.

const bold = 'bold'
const italic = 'italic'

export const claudeCodeInspiredDark: ThemeRegistrationRaw = {
  name: 'claude-code-inspired-dark',
  type: 'dark',
  colors: {
    'editor.background': '#1A1614',
    'editor.foreground': '#F5E6D3',
  },
  settings: [
    {settings: {background: '#1A1614', foreground: '#F5E6D3'}},
    {scope: ['comment', 'punctuation.definition.comment'], settings: {foreground: '#7F7F7F', fontStyle: italic}},
    {
      scope: ['comment.block.documentation', 'comment.block.javadoc', 'string.quoted.docstring'],
      settings: {foreground: '#A08060', fontStyle: italic},
    },
    {scope: ['string', 'string.quoted', 'string.template', 'punctuation.definition.string'], settings: {foreground: '#7FE068'}},
    {scope: ['constant.character.escape', 'string.escape'], settings: {foreground: '#56B6C2'}},
    {scope: ['string.regexp', 'constant.other.character-class.regexp', 'keyword.operator.quantifier.regexp'], settings: {foreground: '#56B6C2'}},
    {scope: ['string.other.link', 'constant.other.symbol'], settings: {foreground: '#FFB38A'}},
    {scope: ['constant.numeric', 'constant.numeric.integer', 'constant.numeric.float', 'constant.numeric.hex'], settings: {foreground: '#FF69B4'}},
    {scope: ['constant', 'constant.other', 'variable.other.constant'], settings: {foreground: '#FFA500'}},
    {scope: ['constant.language', 'support.constant'], settings: {foreground: '#FF9966'}},
    {scope: ['constant.language.boolean', 'constant.language.true', 'constant.language.false'], settings: {foreground: '#E67D22', fontStyle: bold}},
    {
      scope: ['keyword', 'keyword.control', 'storage', 'storage.type', 'storage.modifier'],
      settings: {foreground: '#FF6B35', fontStyle: bold},
    },
    {scope: ['keyword.operator', 'keyword.operator.assignment', 'keyword.operator.arithmetic', 'keyword.operator.comparison', 'keyword.operator.logical'], settings: {foreground: '#FF69B4', fontStyle: ''}},
    {scope: ['keyword.other.directive', 'meta.preprocessor', 'entity.name.function.preprocessor'], settings: {foreground: '#FFB38A', fontStyle: ''}},
    {scope: ['punctuation', 'meta.brace', 'punctuation.definition.parameters'], settings: {foreground: '#8B7355'}},
    {
      scope: ['punctuation.definition.block', 'punctuation.section', 'punctuation.definition.array', 'meta.brace.round', 'meta.brace.square', 'meta.brace.curly'],
      settings: {foreground: '#F5E6D3'},
    },
    {scope: ['punctuation.separator', 'punctuation.terminator', 'punctuation.accessor', 'meta.delimiter'], settings: {foreground: '#ABB2BF'}},
    {scope: ['entity.name.function', 'meta.function-call', 'variable.function'], settings: {foreground: '#FFD700'}},
    {scope: ['support.function', 'support.function.builtin'], settings: {foreground: '#E67D22'}},
    {scope: ['entity.name.function.member', 'meta.method-call', 'meta.method.declaration entity.name.function'], settings: {foreground: '#FFB38A', fontStyle: italic}},
    {scope: ['entity.name.type', 'entity.name.class', 'entity.other.inherited-class', 'support.class', 'support.type'], settings: {foreground: '#00CED1'}},
    {scope: ['support.type.primitive', 'support.type.builtin', 'storage.type.primitive', 'storage.type.built-in'], settings: {foreground: '#71BFFF', fontStyle: ''}},
    {scope: ['entity.name.type.enum', 'entity.name.enum'], settings: {foreground: '#00CED1'}},
    {scope: ['entity.name.function.constructor', 'entity.name.type.constructor', 'meta.new entity.name.type'], settings: {foreground: '#E67D22'}},
    {scope: ['variable', 'variable.other', 'variable.other.readwrite'], settings: {foreground: '#E6B89C'}},
    {scope: ['variable.language', 'variable.language.this', 'variable.language.self', 'variable.language.super'], settings: {foreground: '#E67D22'}},
    {scope: ['variable.parameter', 'meta.parameters variable'], settings: {foreground: '#C4A584', fontStyle: italic}},
    {scope: ['variable.other.property', 'variable.other.object.property', 'support.variable.property', 'meta.object-literal.key', 'entity.name.tag.yaml', 'support.type.property-name'], settings: {foreground: '#FFA07A'}},
    {scope: ['entity.other.attribute-name', 'meta.attribute', 'entity.other.attribute'], settings: {foreground: '#E5C07B'}},
    {scope: ['entity.name.tag', 'meta.tag', 'punctuation.definition.tag'], settings: {foreground: '#E67D22'}},
    {scope: ['entity.name.label', 'entity.name.section', 'storage.modifier.lifetime'], settings: {foreground: '#61AFEF'}},
    {scope: ['entity.name.variant', 'variable.other.enummember', 'constant.other.enum'], settings: {foreground: '#EA9A97'}},
    {scope: ['markup.underline.link', 'string.other.link.description'], settings: {foreground: '#61AFEF', fontStyle: 'underline'}},
    {scope: ['markup.underline.link.url', 'meta.link.inline markup.underline.link', 'string.other.link.destination'], settings: {foreground: '#56B6C2', fontStyle: 'underline'}},
    {scope: ['markup.heading', 'entity.name.section.markdown', 'markup.heading entity.name', 'punctuation.definition.heading'], settings: {foreground: '#E67D22', fontStyle: bold}},
    {scope: ['markup.italic'], settings: {foreground: '#F5E6D3', fontStyle: italic}},
    {scope: ['markup.bold'], settings: {foreground: '#F5E6D3', fontStyle: bold}},
    {scope: ['markup.inline.raw', 'markup.raw', 'markup.fenced_code'], settings: {foreground: '#7FE068'}},
    {scope: ['markup.list punctuation.definition.list', 'punctuation.definition.list.begin'], settings: {foreground: '#E67D22', fontStyle: bold}},
    {scope: ['markup.quote'], settings: {foreground: '#C4A584', fontStyle: italic}},
    {scope: ['markup.inserted', 'meta.diff.header.to-file', 'punctuation.definition.inserted'], settings: {foreground: '#98C379'}},
    {scope: ['markup.deleted', 'meta.diff.header.from-file', 'punctuation.definition.deleted'], settings: {foreground: '#E06C75'}},
    {scope: ['markup.changed', 'meta.diff.range', 'meta.diff.index'], settings: {foreground: '#FFB38A'}},
    {scope: ['meta.embedded', 'source.embedded', 'punctuation.section.embedded'], settings: {foreground: '#98C379'}},
    {scope: ['invalid', 'invalid.illegal'], settings: {foreground: '#E06C75'}},
  ],
}
