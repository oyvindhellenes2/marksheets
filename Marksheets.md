I want to build a web app where you can create pages that is a kind of hybrid between a spreadsheet and a markdown editor.
# Data structure
The data on each page should be in a JSON format

Each object is a header where the header size corresponds with the number of indentation or levels in the JSON file. Top level is the title of the page, second is H1 and so on.

Every line that is not a object (header) should be a list where the first item in the list allways define the list type. There are 5 different types: 
1. Text (default)
2. Image
3. List
4. Data
5. Todo
The types have a predefined template that can be prefilled list items, restrictions on the number of list items, required fields etc. Here is an example:

Gym: {
	Gym equipment: {
		[text, "Her er ei oversikt over utstyr vi skal kjøpe inn til treningsrommet vårt"],
		[todo, "-[ ]", "romaskin","#øyvind"],
		[todo, "-[ ]", "manualar","#øyvind"],
		[data, budsjett, 10000, "kr"]
	}
}

Here the todo type automatically adds a checkbox (in markdown) as the second list item (which is the first item that will be visible on the site). The next item is where you write the todo-text and the last item is a required field where you have to assign a owner of the todo-item (it defaults to the writer, but can be changed).

There need to be a types-file where you can set these templates as you like.

# Features

Linking is supported with standard markdown linking syntax.

The second feature I want in this app is the ability to retrive data by using GET with a special syntax - for instance with "@". When you write "@" followed by the name of a page, you get fetch specific information from other places. Here is two examples:

@gym.gym_equipment.#øyvind will render all the items in gym.gym_equipment which have the hashtag øyvind attached to it.

@gym.gym_equipment.budsjett will render "10000 kr". 

You can not edit data from a GET.
# UI
The UI should be very simple. A simple homepage where you can add and delete pages. 

The page view itself should look similar to a standard markdown editor where the "toolbar" is where the types is defined for each line in the JSON file. One difference from a normal editor is that the editor uses indentations based on level in the JSON file. When you hit the enter button a new json line is created where you can choose a type. Tab shifts one indentation down one level. Double enter moves one indentation back in the hierarchy. Shift-enter creates a new header (object) one same level.

Use a nice looking font. 

All the items should be slightly highlighted with a box or something similar.
# Thoughts
What do you think of this? If you understand what I'm going for (ask me questions if you don't), would you suggest any changes?